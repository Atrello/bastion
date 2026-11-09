// How a stack or container's state is shown: the dot's colour and the words beside it.
import type { Container, GameServer, StackState } from './types';

type Tone = 'green' | 'amber' | 'red' | 'grey';

export const stackStatus: Record<StackState, { tone: Tone; label: string }> = {
  running: { tone: 'green', label: 'Running' },
  partial: { tone: 'amber', label: 'Partly running' },
  stopped: { tone: 'grey', label: 'Stopped' },
  new: { tone: 'grey', label: 'Not started' },
};

export function containerTone(state: Container['state']): Tone {
  if (state === 'running') return 'green';
  if (state === 'restarting' || state === 'paused') return 'amber';
  if (state === 'dead') return 'red';
  return 'grey';
}

export const gameStatus: Record<GameServer['state'], { tone: Tone; label: string }> = {
  running: { tone: 'green', label: 'Running' },
  starting: { tone: 'amber', label: 'Starting' },
  stopping: { tone: 'amber', label: 'Stopping' },
  stopped: { tone: 'grey', label: 'Stopped' },
  crashed: { tone: 'red', label: 'Crashed' },
};
