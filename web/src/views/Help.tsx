import { Dialog } from "../components/Dialog.tsx";
import { t, type Key } from "../i18n/index.ts";

const KEYS: [string, Key][] = [
  ["A", "help.add"], ["/", "help.filter"], ["1 2 3", "help.tabs"], ["↑ ↓", "help.rows"], ["← →", "help.fold"],
  ["Space", "help.select"], ["Ctrl/⌘ + A", "help.selectAll"], ["Del", "help.remove"], ["F2", "help.rename"],
  ["Shift + S", "help.startStop"], ["Shift + P", "help.pause"], ["?", "help.help"],
];

export function HelpDialog({ onClose }: { onClose: () => void }) {
  return (
    <Dialog title={t("help.title")} onClose={onClose}>
      <dl class="keys">
        {KEYS.map(([k, label]) => (
          <div key={k}>
            <dt><kbd>{k}</kbd></dt>
            <dd>{t(label)}</dd>
          </div>
        ))}
      </dl>
    </Dialog>
  );
}
