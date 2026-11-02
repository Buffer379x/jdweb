// Text that comes from outside (file names, paths, comments) is shown as text. One more step for names:
// the invisible controls that reorder characters (right-to-left override and the like) would let a
// name such as "gnp.exe.txt" look like "txt.exe.png", so they are shown as a replacement mark.

const BIDI_CONTROLS = /[‪-‮⁦-⁩‎‏؜]/g;

export function displayText(s: string): string {
  return s.replace(BIDI_CONTROLS, "�");
}
