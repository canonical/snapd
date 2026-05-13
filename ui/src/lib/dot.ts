import type { Change, Status, Task } from '../types/state'

function clusterLabel(lanes: number[]): string {
  return `[${lanes.join(' ')}]`
}

function lanesLess(a: number[], b: number[]): number {
  const n = Math.min(a.length, b.length)
  for (let i = 0; i < n; i++) {
    if (a[i] < b[i]) return -1
    if (a[i] > b[i]) return 1
  }
  return a.length - b.length
}

function sortTasks(tasks: Task[], labels: Map<Task, string>): void {
  tasks.sort((a, b) => {
    const la = labels.get(a)!
    const lb = labels.get(b)!
    if (la < lb) return -1
    if (la > lb) return 1
    return 0
  })
}

function nodeAttrs(status: Status): string[] {
  switch (status) {
    case 'done':
      return ['style=filled', 'fillcolor=lightgreen']
    case 'error':
      return ['style=filled', 'fillcolor=mistyrose']
    case 'undone':
      return ['style=filled', 'fillcolor=moccasin']
    case 'wait':
      return ['style=filled', 'fillcolor=lightblue']
    default:
      return ['style=filled', 'fillcolor=white']
  }
}

export function generateDot(change: Change): string {
  const tasks = [...change.tasks]
  const labels = new Map<Task, string>()

  for (const t of tasks) {
    labels.set(t, `${t.kind}:${t.id}`)
  }

  sortTasks(tasks, labels)

  const clusters: number[][] = []
  const clusterTasks = new Map<string, Task[]>()
  const taskToCluster = new Map<Task, string>()

  for (const t of tasks) {
    const lanes = [...t.lanes].sort((a, b) => a - b)
    const clulabel = clusterLabel(lanes)
    if (!clusterTasks.has(clulabel)) {
      clusters.push(lanes)
    }
    clusterTasks.set(clulabel, [...(clusterTasks.get(clulabel) || []), t])
    taskToCluster.set(t, clulabel)
  }

  clusters.sort(lanesLess)

  // Build halt map from waitFor
  const haltMap = new Map<string, string[]>()
  for (const t of change.tasks) {
    for (const waitId of t.waitFor) {
      if (!haltMap.has(waitId)) {
        haltMap.set(waitId, [])
      }
      haltMap.get(waitId)!.push(t.id)
    }
  }

  const lines: string[] = []
  lines.push('digraph {')

  for (const clu of clusters) {
    const clulabel = clusterLabel(clu)
    lines.push(`subgraph "cluster${clulabel}" {`)
    lines.push(`style=filled; fillcolor="#27272a"; fontcolor=white; color=white; tooltip="Lanes: ${clulabel}"`)
    for (const t of clusterTasks.get(clulabel)!) {
      const attrs = nodeAttrs(t.status)
      const attrStr = attrs.length > 0 ? ` [${attrs.join(', ')}]` : ''
      lines.push(`  "${labels.get(t)}"${attrStr}`)
    }
    lines.push('}')
  }

  for (const t of tasks) {
    const clu = taskToCluster.get(t)!
    const haltIds = haltMap.get(t.id) || []
    const haltTasks = haltIds
      .map((id) => change.tasks.find((t2) => t2.id === id))
      .filter((t2): t2 is Task => t2 !== undefined)

    sortTasks(haltTasks, labels)

    for (const t2 of haltTasks) {
      let attrs = 'color=white'
      if (taskToCluster.get(t2)! !== clu) {
        attrs = 'style=bold, ' + attrs
      }
      const attrStr = ` [${attrs}]`
      lines.push(`"${labels.get(t)}" -> "${labels.get(t2)}"${attrStr}`)
    }
  }

  lines.push('}')
  return lines.join('\n') + '\n'
}
