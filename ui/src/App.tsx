import { ChangeGraph } from './components/ChangeGraph'
import { sampleChange } from './data/sampleChange'

function App() {
  return (
    <div className="bg-zinc-900 min-h-screen p-6">
      <ChangeGraph change={sampleChange} />
    </div>
  )
}

export default App
