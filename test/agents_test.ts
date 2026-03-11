import { assertEquals } from "@std/assert";
import {
  AGENT_NAMES,
  isAgentType,
  shellEscape,
  spawnCommand,
} from "../src/agents.ts";

Deno.test("shellEscape wraps in single quotes", () => {
  assertEquals(shellEscape("hello"), "'hello'");
});

Deno.test("shellEscape escapes embedded single quotes", () => {
  assertEquals(shellEscape("it's"), "'it'\\''s'");
});

Deno.test("shellEscape handles empty string", () => {
  assertEquals(shellEscape(""), "''");
});

Deno.test("shellEscape handles spaces and special chars", () => {
  assertEquals(shellEscape("a b $c"), "'a b $c'");
});

Deno.test("isAgentType returns true for valid agents", () => {
  for (const name of AGENT_NAMES) {
    assertEquals(isAgentType(name), true);
  }
});

Deno.test("isAgentType returns false for unknown agents", () => {
  assertEquals(isAgentType("unknown"), false);
  assertEquals(isAgentType(""), false);
});

Deno.test("isAgentType rejects prototype keys", () => {
  assertEquals(isAgentType("toString"), false);
  assertEquals(isAgentType("hasOwnProperty"), false);
  assertEquals(isAgentType("constructor"), false);
});

Deno.test("AGENT_NAMES contains all supported agents", () => {
  assertEquals(AGENT_NAMES.includes("claude"), true);
  assertEquals(AGENT_NAMES.includes("codex"), true);
  assertEquals(AGENT_NAMES.includes("opencode"), true);
  assertEquals(AGENT_NAMES.includes("gemini"), true);
  assertEquals(AGENT_NAMES.length, 4);
});

Deno.test("spawnCommand generates correct claude command", () => {
  const cmd = spawnCommand("claude", "do stuff");
  assertEquals(cmd, "claude 'do stuff'");
});

Deno.test("spawnCommand generates correct codex command", () => {
  const cmd = spawnCommand("codex", "do stuff");
  assertEquals(cmd, "codex --full-auto 'do stuff'");
});

Deno.test("spawnCommand generates correct opencode command", () => {
  const cmd = spawnCommand("opencode", "do stuff");
  assertEquals(cmd, "opencode run 'do stuff'");
});

Deno.test("spawnCommand generates correct gemini command", () => {
  const cmd = spawnCommand("gemini", "do stuff");
  assertEquals(cmd, "gemini 'do stuff'");
});

Deno.test("spawnCommand escapes prompt with quotes", () => {
  const cmd = spawnCommand("claude", "it's a test");
  assertEquals(cmd, "claude 'it'\\''s a test'");
});
