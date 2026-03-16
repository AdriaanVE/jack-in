/** TUI dashboard: renders live daemon state in the terminal. */

import {
  Computed,
  handleInput,
  handleKeyboardControls,
  handleMouseControls,
  Signal,
  Tui,
} from "tui";
import { Button, Label, type LabelRectangle } from "tui/components";

import type { DaemonContext, WorkerState } from "./daemon.ts";
import { getRingBuffer } from "./log.ts";
import { getJackopsLogger } from "./log.ts";
import * as tmux from "./tmux.ts";

const log = getJackopsLogger("tui");

/** Create a theme with bold-on-selected and 256-color backgrounds. */
function cardTheme(base: number, focused: number, active: number) {
  const make = (bg: number) => (t: string) =>
    t.includes(">")
      ? `\x1b[1;37;48;5;${bg}m${t}\x1b[0m`
      : `\x1b[37;48;5;${bg}m${t}\x1b[0m`;
  return { base: make(base), focused: make(focused), active: make(active) };
}

/** Underline the first occurrence of `char` in the styled output. */
function underlineChar(
  styled: string,
  char: string,
): string {
  const i = styled.indexOf(char);
  if (i === -1) return styled;
  return styled.slice(0, i) + `\x1b[4m${char}\x1b[24m` +
    styled.slice(i + char.length);
}

const STATE_ICONS: Record<string, string> = {
  idle: "○",
  working: "●",
  stuck: "!",
};

function workerState(w: WorkerState): string {
  if (w.escalatedToUser) return "stuck";
  if (w.currentTask) return "working";
  return "idle";
}

export interface DashboardOptions {
  ctx: DaemonContext;
  onDown: () => void;
}

export function createDashboard(
  opts: DashboardOptions,
): { tui: Tui; destroy: () => void } {
  const { ctx, onDown } = opts;

  const tui = new Tui({ refreshRate: 1000 / 2 });

  handleInput(tui);
  handleMouseControls(tui);
  handleKeyboardControls(tui);

  const selectedWorker = new Signal(0);
  const workerNames = [...ctx.workers.keys()];

  let bottomRole = "orchestrator";

  // --- Header ---
  const headerText = new Signal(
    `JACKOPS -- ${ctx.session}  [${ctx.approval}]  arrows:select  enter:swap  o:orch  c:cli`,
  );
  new Label({
    parent: tui,
    zIndex: 0,
    rectangle: { column: 1, row: 0 },
    text: headerText,
    align: { horizontal: "left", vertical: "top" },
    theme: { base: (t: string) => `\x1b[1m${t}\x1b[0m` },
  });

  // --- Worker cards: Button (bg + click) + Label overlay (text) ---
  const CARD_WIDTH = 32;
  const CARD_HEIGHT = 3;
  const CARD_ROW = 2;

  function getWorkerLabel(name: string): string {
    const w = ctx.workers.get(name);
    if (!w) return `? ${name}`;
    const state = workerState(w);
    const icon = STATE_ICONS[state] ?? "?";
    const task = w.currentTask ? ` ${w.currentTask}` : "";
    return `${icon} ${name} [${state}]${task}`;
  }

  function cardText(label: string, width: number, selected: boolean): string {
    const sel = selected ? ">" : " ";
    const inner = `${sel} ${label}`.padEnd(width - 2).slice(0, width - 2);
    const horiz = "─".repeat(width - 2);
    return `┌${horiz}┐\n│${inner}│\n└${horiz}┘`;
  }

  const cardSignals: Signal<string>[] = [];

  for (let i = 0; i < workerNames.length; i++) {
    const idx = i;
    const name = workerNames[idx];
    const col = 1 + idx * CARD_WIDTH;
    const sig = new Signal(
      cardText(getWorkerLabel(name), CARD_WIDTH, idx === 0),
    );
    cardSignals.push(sig);

    // Button: invisible background, click target only (Label handles colors)
    const btn = new Button({
      parent: tui,
      zIndex: 1,
      rectangle: {
        column: col,
        row: CARD_ROW,
        width: CARD_WIDTH,
        height: CARD_HEIGHT,
      },
      theme: { base: (t: string) => t },
    });

    // Label: text overlay, shares Button's rectangle and state so colors match
    const workerLabel = new Label({
      parent: btn,
      zIndex: 2,
      rectangle: btn.rectangle as unknown as Signal<LabelRectangle>,
      overwriteRectangle: true,
      text: sig,
      align: { horizontal: "left", vertical: "top" },
      theme: cardTheme(18, 20, 21),
    });
    workerLabel.state = btn.state;
    workerLabel.style = new Computed(() => workerLabel.theme[btn.state.value]);

    btn.on("mousePress", () => {
      selectedWorker.value = idx;
      showRole(ctx.session, name);
    });
  }

  // Orchestrator card
  let orchCardSignal: Signal<string> | null = null;
  if (ctx.orchState) {
    const orchIdx = workerNames.length;
    const col = 1 + orchIdx * CARD_WIDTH;
    const sig = new Signal(
      cardText("● orch [active]", CARD_WIDTH, orchIdx === 0),
    );
    orchCardSignal = sig;
    cardSignals.push(sig);

    const btn = new Button({
      parent: tui,
      zIndex: 1,
      rectangle: {
        column: col,
        row: CARD_ROW,
        width: CARD_WIDTH,
        height: CARD_HEIGHT,
      },
      theme: { base: (t: string) => t },
    });

    const orchLabel = new Label({
      parent: btn,
      zIndex: 2,
      rectangle: btn.rectangle as unknown as Signal<LabelRectangle>,
      overwriteRectangle: true,
      text: sig,
      align: { horizontal: "left", vertical: "top" },
      theme: {
        base: (t: string) => underlineChar(`\x1b[37;48;5;54m${t}\x1b[0m`, "o"),
        focused: (t: string) =>
          underlineChar(`\x1b[37;48;5;55m${t}\x1b[0m`, "o"),
        active: (t: string) =>
          underlineChar(`\x1b[1;37;48;5;56m${t}\x1b[0m`, "o"),
      },
    });
    orchLabel.state = btn.state;
    orchLabel.style = new Computed(() => orchLabel.theme[btn.state.value]);

    btn.on("mousePress", () => {
      selectedWorker.value = orchIdx;
      showRole(ctx.session, "orchestrator");
    });
  }

  const totalCards = workerNames.length + (ctx.orchState ? 1 : 0);

  // --- Task counters ---
  const taskText = new Signal(formatTaskCounts(ctx));
  new Label({
    parent: tui,
    zIndex: 0,
    rectangle: { column: 1, row: CARD_ROW + 4 },
    text: taskText,
    align: { horizontal: "left", vertical: "top" },
    theme: { base: (t: string) => `\x1b[33m${t}\x1b[0m` },
  });

  // --- CLI button (task counter row, right side) ---
  const cliBtn = new Button({
    parent: tui,
    zIndex: 1,
    rectangle: { column: 65, row: CARD_ROW + 4, width: 8, height: 1 },
    theme: { base: (t: string) => t },
  });
  const cliTheme = {
    base: (t: string) => underlineChar(`\x1b[37;48;5;24m${t}\x1b[0m`, "c"),
    focused: (t: string) => underlineChar(`\x1b[37;48;5;31m${t}\x1b[0m`, "c"),
    active: (t: string) => underlineChar(`\x1b[1;37;48;5;38m${t}\x1b[0m`, "c"),
  };
  const cliLabel = new Label({
    parent: cliBtn,
    zIndex: 2,
    rectangle: cliBtn.rectangle as unknown as Signal<LabelRectangle>,
    overwriteRectangle: true,
    text: new Signal(" [cli] "),
    align: { horizontal: "left", vertical: "top" },
    theme: cliTheme,
  });
  cliLabel.state = cliBtn.state;
  cliLabel.style = new Computed(() => cliLabel.theme[cliBtn.state.value]);
  cliBtn.on("mousePress", () => openCli());

  // --- Down button (task counter row, right side) ---
  const downBtn = new Button({
    parent: tui,
    zIndex: 1,
    rectangle: { column: 75, row: CARD_ROW + 4, width: 14, height: 1 },
    theme: { base: (t: string) => t },
  });
  const downTheme = {
    base: (t: string) => underlineChar(`\x1b[37;48;5;52m${t}\x1b[0m`, "d"),
    focused: (t: string) => underlineChar(`\x1b[37;48;5;88m${t}\x1b[0m`, "d"),
    active: (t: string) => underlineChar(`\x1b[1;37;48;5;124m${t}\x1b[0m`, "d"),
  };
  const downLabel = new Label({
    parent: downBtn,
    zIndex: 2,
    rectangle: downBtn.rectangle as unknown as Signal<LabelRectangle>,
    overwriteRectangle: true,
    text: new Signal(" [down (quit)]"),
    align: { horizontal: "left", vertical: "top" },
    theme: downTheme,
  });
  downLabel.state = downBtn.state;
  downLabel.style = new Computed(() => downLabel.theme[downBtn.state.value]);
  downBtn.on("mousePress", () => onDown());

  // --- Log feed ---
  const logText = new Signal("");
  new Label({
    parent: tui,
    zIndex: 0,
    rectangle: { column: 1, row: CARD_ROW + 6, width: 120, height: 14 },
    text: logText,
    align: { horizontal: "left", vertical: "top" },
    theme: { base: (t: string) => `\x1b[90m${t}\x1b[0m` },
  });

  // --- Tick: update all signals every 500ms ---
  function updateAll() {
    headerText.value =
      `JACKOPS -- ${ctx.session}  [${ctx.approval}]  arrows:select  enter:swap  o:orch  c:cli`;

    for (let i = 0; i < workerNames.length; i++) {
      cardSignals[i].value = cardText(
        getWorkerLabel(workerNames[i]),
        CARD_WIDTH,
        selectedWorker.value === i,
      );
    }
    if (orchCardSignal) {
      orchCardSignal.value = cardText(
        "● orch [active]",
        CARD_WIDTH,
        selectedWorker.value === workerNames.length,
      );
    }

    taskText.value = formatTaskCounts(ctx);

    const buf = getRingBuffer();
    logText.value = buf.slice(-14).reverse().join("\n");
  }

  const tickTimer = setInterval(updateAll, 500);
  setTimeout(updateAll, 50);

  // --- Keyboard handling ---
  tui.on("keyPress", (event) => {
    if (
      event.key === "q" || event.key === "d" ||
      (event.ctrl && event.key === "c")
    ) {
      onDown();
      return;
    }
    if (event.key === "left" || event.key === "h") {
      selectedWorker.value = Math.max(0, selectedWorker.value - 1);
      updateAll();
    }
    if (event.key === "right" || event.key === "l") {
      selectedWorker.value = Math.min(totalCards - 1, selectedWorker.value + 1);
      updateAll();
    }
    if (event.key === "c") {
      openCli();
      return;
    }
    if (event.key === "o" && ctx.orchState) {
      selectedWorker.value = workerNames.length;
      showRole(ctx.session, "orchestrator");
      updateAll();
      return;
    }
    if (event.key === "return") {
      const idx = selectedWorker.value;
      if (idx < workerNames.length) {
        showRole(ctx.session, workerNames[idx]);
      } else if (ctx.orchState) {
        showRole(ctx.session, "orchestrator");
      }
    }
  });

  // --- CLI pane ---
  async function ensureCliPane(session: string): Promise<void> {
    const existing = await tmux.findPaneByRole(session, "cli");
    if (existing) return;
    await tmux.createWindow(session, "cli");
    await tmux.setPaneOption(`${session}:cli.0`, "@jackops_role", "cli");
    await tmux.selectWindow(session, "dashboard-orchestrator");
  }

  function openCli(): void {
    ensureCliPane(ctx.session).then(() => showRole(ctx.session, "cli")).catch(
      (e) => log.warn`[cli] failed: ${e instanceof Error ? e.message : e}`,
    );
  }

  // --- Swap logic ---
  let swapping = false;

  async function showRole(session: string, targetRole: string): Promise<void> {
    if (targetRole === bottomRole) return;
    if (swapping) return;
    swapping = true;

    try {
      const orchId = await tmux.findPaneByRole(session, "orchestrator");
      if (!orchId) return;

      if (bottomRole !== "orchestrator") {
        const currentId = await tmux.findPaneByRole(session, bottomRole);
        if (currentId) {
          await tmux.swapPane(currentId, orchId);
          log
            .debug`[swap] restored ${bottomRole} (${currentId}) <-> orchestrator (${orchId})`;
        }
        bottomRole = "orchestrator";
      }

      if (targetRole !== "orchestrator") {
        const targetId = await tmux.findPaneByRole(session, targetRole);
        if (targetId) {
          await tmux.swapPane(targetId, orchId);
          log
            .debug`[swap] showing ${targetRole} (${targetId}) <-> orchestrator (${orchId})`;
          bottomRole = targetRole;
        }
      }
    } catch (e) {
      log.warn`[swap] failed: ${e instanceof Error ? e.message : e}`;
    } finally {
      swapping = false;
    }
  }

  function destroy() {
    clearInterval(tickTimer);
    tui.destroy();
  }

  return { tui, destroy };
}

function formatTaskCounts(ctx: DaemonContext): string {
  const c = ctx.taskCounts;
  return `Tasks: ${c.pending} pending  ${c.current} current  ${c.review} review  ${c.complete} complete  ${c.rejected} rejected`;
}
