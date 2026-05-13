import type { Change, Task } from '../types/state'

// Replicates the task graph from overlord/state/dot/dot_test.go with
// varied statuses so colours are visible in the rendered graph.
const tasks: Task[] = [
  {
    id: '1',
    kind: 'a',
    summary: 'a1',
    status: 'done',
    lanes: [1],
    waitFor: [],
  },
  {
    id: '2',
    kind: 'a',
    summary: 'a2',
    status: 'doing',
    lanes: [1],
    waitFor: ['1'],
  },
  {
    id: '3',
    kind: 'b',
    summary: 'b',
    status: 'do',
    lanes: [1, 2],
    waitFor: ['1', '2'],
  },
  {
    id: '4',
    kind: 'c',
    summary: 'c',
    status: 'wait',
    waitedStatus: 'done',
    lanes: [2],
    waitFor: ['3'],
  },
  {
    id: '5',
    kind: 'd',
    summary: 'd',
    status: 'error',
    lanes: [0],
    waitFor: [],
  },
]

export const sampleChange: Change = {
  id: '1',
  kind: 'install',
  summary: 'test install',
  tasks,
}
