# TODO

## Code review findings (deferred)

Issues identified during review that are not worth fixing in the MVP but should be revisited as the codebase grows.

### From internal review (Claude + Codex)

**cli.ts**
- [ ] Extract config loading helper -- `findConfig()` + `loadConfig()` repeated in 5 functions
- [ ] Extract worker validation helper -- lookup + error message duplicated in `send()` and `attach()`
- [ ] Extract session existence check -- same guard in `send()` and `attach()`
- [ ] Stdin prompt in `down()` reads 4-byte buffer -- fragile, replace with readline when adding more prompts
- [ ] Partial startup failure leaves orphaned tmux session -- consider rollback or clearer recovery guidance

**tmux.ts**
- [ ] 9x inline `if (!success) throw new Error(...)` -- extract `assertSuccess()` helper when adding more commands
- [ ] Tab-split parsing of tmux output has no validation on field count
- [ ] `capturePane` default of 50 lines is a magic number

**worktree.ts**
- [ ] Git retry logic for "branch already exists" is hardcoded in `create()` -- consider cleaner recovery
- [ ] Sequential worktree removal in `cleanup()` -- could parallelize if git supports it safely

**status.ts**
- [ ] Shell detection list `["bash", "zsh", "fish", "sh"]` should be a constant
- [ ] `hasSession()` and `listPanes()` called sequentially -- could parallel with `Promise.all()`

**General**
- [ ] `attach()` uses raw `Deno.Command` instead of tmux module -- intentional (needs inherited stdio), but add an interactive variant to `tmux.ts` if more interactive commands are added
- [ ] No integration tests -- add after MVP validation

### From Codex review

- [ ] `down` requires config file for session cleanup -- works without it for worktrees, but can't kill tmux session without knowing project name. Consider storing project name in tmux session environment variable.
- [ ] Status shows "idle" when agent exited but shell is still open -- accurate but might confuse users expecting "done"
