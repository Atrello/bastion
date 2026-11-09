// Terminal colour codes in logs and game consoles ("\x1b[33m"): read into coloured pieces of text,
// so they show as colours (from the theme) instead of as codes. Anything else in a code is dropped.

export type Piece = { text: string; className: string };

const CODE = /\x1b\[([0-9;]*)m|\x1b\[[0-9;?]*[A-Za-z]/g;

export function colourPieces(line: string): Piece[] {
  const pieces: Piece[] = [];
  let fg = 0;
  let bold = false;
  let last = 0;
  const push = (text: string) => {
    if (text) pieces.push({ text, className: [fg ? `ansi-${fg}` : '', bold ? 'ansi-bold' : ''].join(' ').trim() });
  };
  for (const m of line.matchAll(CODE)) {
    push(line.slice(last, m.index));
    last = m.index + m[0].length;
    if (m[1] === undefined) continue; // not a colour: cursor movement and the like
    for (const part of (m[1] || '0').split(';').map(Number)) {
      if (part === 0) (fg = 0), (bold = false);
      else if (part === 1) bold = true;
      else if (part === 22) bold = false;
      else if (part === 39) fg = 0;
      else if ((part >= 30 && part <= 37) || (part >= 90 && part <= 97)) fg = part;
    }
  }
  push(line.slice(last));
  return pieces;
}
