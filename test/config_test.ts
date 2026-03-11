import { assertEquals, assertRejects } from "@std/assert";
import { loadConfig, sessionName } from "../src/config.ts";

Deno.test("sessionName prefixes with jackops-", () => {
  assertEquals(sessionName("my-app"), "jackops-my-app");
});

async function withTempConfig(
  yaml: string,
  fn: (path: string) => Promise<void>,
) {
  const dir = await Deno.makeTempDir();
  const path = `${dir}/jackops.yaml`;
  await Deno.writeTextFile(path, yaml);
  try {
    await fn(path);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
}

Deno.test("loadConfig parses valid config", async () => {
  await withTempConfig(
    `
project: my-app
workers:
  - name: backend
    agent: claude
    prompt: "do backend"
  - name: frontend
    agent: codex
    prompt: "do frontend"
`,
    async (path) => {
      const config = await loadConfig(path);
      assertEquals(config.project, "my-app");
      assertEquals(config.workers.length, 2);
      assertEquals(config.workers[0].name, "backend");
      assertEquals(config.workers[0].agent, "claude");
      assertEquals(config.workers[0].prompt, "do backend");
      assertEquals(config.workers[1].name, "frontend");
      assertEquals(config.workers[1].agent, "codex");
    },
  );
});

Deno.test("loadConfig rejects missing project", async () => {
  await withTempConfig(
    `
workers:
  - name: w1
    agent: claude
    prompt: "do stuff"
`,
    async (path) => {
      await assertRejects(
        () => loadConfig(path),
        Error,
        "project",
      );
    },
  );
});

Deno.test("loadConfig rejects empty workers", async () => {
  await withTempConfig(
    `
project: my-app
workers: []
`,
    async (path) => {
      await assertRejects(
        () => loadConfig(path),
        Error,
        "workers",
      );
    },
  );
});

Deno.test("loadConfig rejects invalid agent type", async () => {
  await withTempConfig(
    `
project: my-app
workers:
  - name: w1
    agent: gpt4
    prompt: "do stuff"
`,
    async (path) => {
      await assertRejects(
        () => loadConfig(path),
        Error,
        "agent",
      );
    },
  );
});

Deno.test("loadConfig rejects duplicate worker names", async () => {
  await withTempConfig(
    `
project: my-app
workers:
  - name: w1
    agent: claude
    prompt: "do stuff"
  - name: w1
    agent: codex
    prompt: "do more"
`,
    async (path) => {
      await assertRejects(
        () => loadConfig(path),
        Error,
        "Duplicate",
      );
    },
  );
});

Deno.test("loadConfig rejects unsafe project name", async () => {
  await withTempConfig(
    `
project: "my app/bad"
workers:
  - name: w1
    agent: claude
    prompt: "do stuff"
`,
    async (path) => {
      await assertRejects(
        () => loadConfig(path),
        Error,
        "alphanumeric",
      );
    },
  );
});

Deno.test("loadConfig rejects unsafe worker name", async () => {
  await withTempConfig(
    `
project: my-app
workers:
  - name: "w:1"
    agent: claude
    prompt: "do stuff"
`,
    async (path) => {
      await assertRejects(
        () => loadConfig(path),
        Error,
        "alphanumeric",
      );
    },
  );
});

Deno.test("loadConfig rejects missing prompt", async () => {
  await withTempConfig(
    `
project: my-app
workers:
  - name: w1
    agent: claude
`,
    async (path) => {
      await assertRejects(
        () => loadConfig(path),
        Error,
        "prompt",
      );
    },
  );
});
