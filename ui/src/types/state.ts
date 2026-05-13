export type Status =
  | 'default'
  | 'hold'
  | 'do'
  | 'doing'
  | 'done'
  | 'abort'
  | 'undo'
  | 'undoing'
  | 'undone'
  | 'error'
  | 'wait'

export interface Task {
  id: string
  kind: string
  summary: string
  status: Status
  waitedStatus?: 'done' | 'undone'
  lanes: number[]
  waitFor: string[]
}

export interface Change {
  id: string
  kind: string
  summary: string
  tasks: Task[]
}
