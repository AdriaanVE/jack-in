import { assertEquals } from "@std/assert";
import { assemblePrompt } from "../src/init.ts";
import type { AgentType } from "../src/agents.ts";

Deno.test("assemblePrompt includes all sections", () => {
  const result = assemblePrompt({
    instructions: "# Test Instructions\nDo the thing.",
    agents: ["claude", "codex"] as AgentType[],
    projectName: "my-app",
    readme: "# My App\nA cool project.",
    files: "./src\n./src/main.ts",
  });

  // Contains static instructions
  assertEquals(result.includes("# Test Instructions"), true);
  // Contains available agents
  assertEquals(result.includes("**AVAILABLE AGENTS:** claude, codex"), true);
  // Contains project name
  assertEquals(result.includes("**SUGGESTED PROJECT NAME:** my-app"), true);
  // Contains README
  assertEquals(result.includes("# My App"), true);
  // Contains file listing
  assertEquals(result.includes("./src/main.ts"), true);
});

Deno.test("assemblePrompt handles missing README", () => {
  const result = assemblePrompt({
    instructions: "# Instructions",
    agents: ["claude"] as AgentType[],
    projectName: "test",
    readme: null,
    files: "./src",
  });

  assertEquals(result.includes("No README.md found"), true);
});

Deno.test("assemblePrompt with single agent", () => {
  const result = assemblePrompt({
    instructions: "# Init",
    agents: ["gemini"] as AgentType[],
    projectName: "solo",
    readme: "# Solo\nJust me.",
    files: ".",
  });

  assertEquals(result.includes("**AVAILABLE AGENTS:** gemini"), true);
  assertEquals(result.includes("**SUGGESTED PROJECT NAME:** solo"), true);
});
