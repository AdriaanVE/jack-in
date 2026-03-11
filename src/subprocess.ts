/** Shared subprocess runner. */

const decoder = new TextDecoder();

export interface RunResult {
  success: boolean;
  stdout: string;
  stderr: string;
}

export async function exec(
  bin: string,
  args: string[],
  cwd?: string,
): Promise<RunResult> {
  const cmd = new Deno.Command(bin, {
    args,
    cwd,
    stdout: "piped",
    stderr: "piped",
  });
  const output = await cmd.output();
  return {
    success: output.success,
    stdout: decoder.decode(output.stdout).trimEnd(),
    stderr: decoder.decode(output.stderr).trimEnd(),
  };
}
