// A live stream from the panel (logs or a console) over a websocket: text arrives through ondata until
// the stream ends; send() passes keys and sizes back for a console.

export type Stream = { send: (message: object) => void; close: () => void };

export function openStream(path: string, ondata: (text: string) => void, onend: (reason?: string) => void, onopen?: () => void): Stream {
  const ws = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/streams/${path}`);
  let ended = false;
  const end = (reason?: string) => {
    if (!ended) {
      ended = true;
      onend(reason);
    }
  };
  ws.onmessage = (e) => {
    const m = JSON.parse(e.data) as { type: string; data?: string };
    if (m.type === 'data') ondata(m.data ?? '');
    else end();
  };
  ws.onopen = () => onopen?.();
  ws.onclose = () => end('The connection to the server closed.');
  return {
    send: (message) => ws.readyState === WebSocket.OPEN && ws.send(JSON.stringify(message)),
    close: () => {
      ended = true;
      ws.close();
    },
  };
}
