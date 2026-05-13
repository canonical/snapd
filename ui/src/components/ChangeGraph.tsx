import { useEffect, useRef } from 'react'
import type { Change } from '../types/state'
import { generateDot } from '../lib/dot'
import { getViz } from '../lib/viz'

interface ChangeGraphProps {
  change: Change
}

export function ChangeGraph({ change }: ChangeGraphProps) {
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let cancelled = false

    async function render() {
      const viz = await getViz()
      if (cancelled) return

      const dot = generateDot(change)
      const svg = viz.renderSVGElement(dot, {
        graphAttributes: {
          nodesep: 0.4,
          ranksep: 0.8,
          pad: 0.3,
          bgcolor: 'transparent',
          fontcolor: 'white',
        },
        nodeAttributes: { fontcolor: 'white' },
      })
      if (cancelled) return

      const container = containerRef.current
      if (!container) return

      container.replaceChildren(svg)
    }

    render().catch((err) => {
      if (!cancelled) {
        console.error('cannot render change graph:', err)
      }
    })

    return () => {
      cancelled = true
    }
  }, [change])

  return (
    <div
      ref={containerRef}
      style={{ overflow: 'auto', width: '100%', minHeight: '400px' }}
    />
  )
}
