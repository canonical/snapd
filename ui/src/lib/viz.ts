import { instance } from '@viz-js/viz'

let vizPromise: ReturnType<typeof instance> | null = null

export function getViz() {
  if (!vizPromise) {
    vizPromise = instance()
  }
  return vizPromise
}
