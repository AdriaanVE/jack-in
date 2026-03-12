/** jackops init — LLM-driven interactive setup via tmux. */

import { basename, dirname, fromFileUrl, join } from "@std/path";
import {
  AGENT_PREFERENCE,
  type AgentType,
  detectAgents,
  initCommand,
  shellEscape,
} from "./agents.ts";
import * as tmux from "./tmux.ts";

const MAX_README_LINES = 500;
const MAX_FILE_LISTING = 80;
const INIT_SESSION = "jackops-init";

export interface InitOpts {
  agent?: AgentType;
}

/** Read the target repo's README.md, truncated. */
async function readReadme(base: string): Promise<string | null> {
  for (const name of ["README.md", "readme.md", "Readme.md"]) {
    try {
      const text = await Deno.readTextFile(join(base, name));
      const lines = text.split("\n");
      if (lines.length > MAX_README_LINES) {
        return lines.slice(0, MAX_README_LINES).join("\n") +
          `\n\n(truncated — showing first ${MAX_README_LINES} of ${lines.length} lines)`;
      }
      return text;
    } catch (e) {
      if (e instanceof Deno.errors.NotFound) continue;
      throw e;
    }
  }
  return null;
}

/** Get a shallow file listing of the repo. */
async function fileTree(base: string): Promise<string> {
  const cmd = new Deno.Command("find", {
    args: [
      ".",
      "-maxdepth",
      "2",
      "-not",
      "-path",
      "./.git/*",
      "-not",
      "-path",
      "./node_modules/*",
      "-not",
      "-path",
      "./.w-*",
    ],
    cwd: base,
    stdout: "piped",
    stderr: "null",
  });
  const { stdout } = await cmd.output();
  const lines = new TextDecoder().decode(stdout).trim().split("\n");
  return lines.slice(0, MAX_FILE_LISTING).join("\n");
}

/** Read the static init instructions template with skill content injected. */
async function readInstructions(): Promise<string> {
  const moduleDir = dirname(fromFileUrl(import.meta.url));
  const repoRoot = join(moduleDir, "..");
  const [template, skill] = await Promise.all([
    Deno.readTextFile(join(repoRoot, "docs", "init-instructions.md")),
    Deno.readTextFile(
      join(repoRoot, ".claude", "skills", "jackops", "SKILL.md"),
    ),
  ]);
  return template.replace("{SKILL_CONTENT}", skill);
}

/** Assemble the full prompt from template + runtime context. */
export function assemblePrompt(opts: {
  instructions: string;
  agents: AgentType[];
  projectName: string;
  readme: string | null;
  files: string;
}): string {
  const sections = [opts.instructions];

  sections.push(
    `\n---\n\n## Runtime context\n`,
    `**AVAILABLE AGENTS:** ${opts.agents.join(", ")}`,
    `**SUGGESTED PROJECT NAME:** ${opts.projectName}`,
  );

  if (opts.readme) {
    sections.push(`\n### Project README\n\n${opts.readme}`);
  } else {
    sections.push(
      `\n### Project README\n\nNo README.md found in this project.`,
    );
  }

  sections.push(`\n### File listing\n\n\`\`\`\n${opts.files}\n\`\`\``);

  return sections.join("\n");
}

/** Check if jackops.yaml already exists. */
async function configExists(base: string): Promise<string | null> {
  for (const name of ["jackops.yaml", "jackops.yml"]) {
    try {
      await Deno.stat(join(base, name));
      return name;
    } catch (e) {
      if (e instanceof Deno.errors.NotFound) continue;
      throw e;
    }
  }
  return null;
}

function prompt(message: string): Promise<string> {
  const buf = new Uint8Array(4);
  return (async () => {
    await Deno.stdout.write(new TextEncoder().encode(message));
    const n = await Deno.stdin.read(buf);
    return new TextDecoder().decode(buf.subarray(0, n ?? 0)).trim()
      .toLowerCase();
  })();
}

/** Attach to the init tmux session (or switch if already inside tmux). */
async function attachSession(): Promise<void> {
  if (Deno.env.get("TMUX")) {
    await tmux.selectWindow(INIT_SESSION, "init");
  } else {
    const cmd = new Deno.Command("tmux", {
      args: ["attach", "-t", INIT_SESSION],
      stdin: "inherit",
      stdout: "inherit",
      stderr: "inherit",
    });
    const { code } = await cmd.spawn().status;
    Deno.exit(code);
  }
}

/** Main init flow. */
export async function init(opts: InitOpts): Promise<void> {
  const base = Deno.cwd();

  // Check for non-interactive terminal
  if (!Deno.stdin.isTerminal()) {
    console.error(
      "jackops init requires an interactive terminal. Use jackops init --template for a non-interactive config.",
    );
    Deno.exit(1);
  }

  // If init session already exists, offer to attach
  if (await tmux.hasSession(INIT_SESSION)) {
    const answer = await prompt(
      `Init session already running. [A]ttach, [R]estart, [Q]uit? [a/r/Q] `,
    );
    if (answer === "a") {
      await attachSession();
      return;
    } else if (answer === "r") {
      await tmux.killSession(INIT_SESSION);
    } else {
      console.log("Aborted.");
      Deno.exit(0);
    }
  }

  // Check for existing config
  const existing = await configExists(base);
  if (existing) {
    const answer = await prompt(
      `${existing} already exists. [O]verwrite, [A]bort? [o/A] `,
    );
    if (answer !== "o") {
      console.log("Aborted.");
      Deno.exit(0);
    }
  }

  // Detect agents
  console.log("Detecting installed agents...");
  let agents: AgentType[];
  if (opts.agent) {
    agents = [opts.agent];
    console.log(`  Using specified agent: ${opts.agent}`);
  } else {
    agents = await detectAgents();
    if (agents.length === 0) {
      console.error(
        "No agent CLIs found on PATH. Install at least one of:",
      );
      for (const a of AGENT_PREFERENCE) {
        console.error(`  - ${a}`);
      }
      Deno.exit(1);
    }
    console.log(`  Found: ${agents.join(", ")}`);
  }

  // Pick the agent to run init with
  const initAgent = opts.agent ?? agents[0];

  // Read project context
  console.log("Reading project context...");
  const [readme, files, instructions] = await Promise.all([
    readReadme(base),
    fileTree(base),
    readInstructions(),
  ]);

  const projectName = basename(base).replace(/[^a-zA-Z0-9_-]/g, "-");

  // Assemble and write prompt to temp file
  const fullPrompt = assemblePrompt({
    instructions,
    agents,
    projectName,
    readme,
    files,
  });

  const promptDir = join(base, ".jackops");
  await Deno.mkdir(promptDir, { recursive: true });
  const promptFile = join(promptDir, "init-prompt.md");
  await Deno.writeTextFile(promptFile, fullPrompt);

  // Create tmux session and spawn agent
  console.log(`\nSpawning ${initAgent} in tmux session '${INIT_SESSION}'...`);
  await tmux.createSession(INIT_SESSION);
  await tmux.renameWindow(INIT_SESSION, 0, "init");

  const target = `${INIT_SESSION}:init`;
  const cmd = `cd ${shellEscape(base)} && ${
    initCommand(initAgent, promptFile)
  }`;
  await tmux.sendKeys(target, cmd);

  console.log(`Attaching to session...\n`);
  await attachSession();
}

/** Write a template config without an LLM. */
export async function initTemplate(): Promise<void> {
  const base = Deno.cwd();
  const projectName = basename(base).replace(/[^a-zA-Z0-9_-]/g, "-");

  const template = `# jackops.yaml — generated template
# Edit this file, then run: jackops up

project: ${projectName}

workers:
  - name: coder
    agent: claude        # claude | codex | opencode | gemini
    prompt: "implement features and fix bugs"
    role: executor       # executor | reviewer | planner

# orchestrator:
#   poll_interval: 5000  # ms
#   max_retries: 2
#   approval: manual     # manual | auto | yolo

# tasks:
#   - summary: "Explore the codebase"
#   - summary: "Add tests for core modules"
#     depends_on: ["Explore the codebase"]
`;

  const existing = await configExists(base);
  if (existing) {
    console.error(
      `${existing} already exists. Remove it first or run 'jackops init' to overwrite interactively.`,
    );
    Deno.exit(1);
  }

  await Deno.writeTextFile(join(base, "jackops.yaml"), template);
  console.log("Written ./jackops.yaml (template)");
  console.log("Edit the file, then run: jackops up");
}
