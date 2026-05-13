# Agent Skill Engineering: Best Practices & Design Patterns

## 1. The Core Philosophy: The Context Economy
The central challenge in modern agent architecture is the **Context Economy**. Every token loaded into an agent's context window consumes resources and dilutes attention.
* **The Constraint:** "Noisy" context leads to degraded reasoning. If an agent is overwhelmed with irrelevant data, it hallucinates or fails to follow instructions.
* **The Solution:** **Progressive Disclosure**. Information must only be revealed when strictly necessary for the immediate task.
    * *Tier 1 (Metadata):* Loaded at boot. Minimal cost.
    * *Tier 2 (Orchestration):* Loaded at activation (`SKILL.md`).
    * *Tier 3 (Deep Knowledge):* Loaded on-demand (`references/`).
    * *Tier 4 (Execution):* Context-free compute (`scripts/`).

## 2. Naming Strategy: The Semantic First Impression
A skill's name is not just a label; it is the first signal of intent sent to the agent.
* **Use Gerunds:** Names should imply an *active process*. Prefer `processing-pdfs` over `pdf-tool`. This aligns with the agent's internal model of tools as "functions to be executed".
* **Avoid Generics:** Names like `data-helper` or `utils` are architecturally weak. They lack high-entropy signals, making it difficult for the router to select them correctly.

## 3. Description Engineering: The Discovery Layer
The `description` field in `SKILL.md` is the most critical element in the entire skill. If the description fails, the skill is never routed.
* **Perspective:** Always write in the **Third Person**.
    * *Good:* "Extracts financial data from..."
    * *Bad:* "I can help you extract...".
* **The "Trigger" Pattern:** Explicitly state the condition that should trigger the skill.
    * *Formula:* "Use this skill when [USER INPUT TRIGGER] or [TASK GOAL]."
    * *Example:* "Use this skill when the user provides a PDF document or asks for form analysis".
* **Keyword Density:** Stuff the description with specific input/output nouns (e.g., "JSON", "Audit", "Compliance", "Merge"). The router matches these against the user's prompt.

## 4. The Instruction Strategy: The Freedom Scale
Do not write all instructions the same way. Tune the "Freedom Level" based on the risk profile of the task.

| Freedom Level | Use Case | Style | Example Phrase |
| :--- | :--- | :--- | :--- |
| **High** | Creative Writing, Ideation | Guiding Heuristics | "Draft a response... Ensure tone is professional." |
| **Medium** | Data Analysis, Summarization | Pseudo-code steps | "1. Analyze headers. 2. Identify missing values." |
| **Low** | Database Writes, File Deletion | Strict Commands | "Run `scripts/delete.py`. Do not attempt manual deletion." |

## 5. Architecture: The "Router" Pattern
The `SKILL.md` file should act as a **Router**, not a Encyclopedia.
* **The Goal:** It should contain instructions on *how to find* knowledge, not the knowledge itself.
* **Implementation:**
    * *Incorrect:* Listing 50 pages of tax codes in `SKILL.md`.
    * *Correct:* "If the user asks about tax codes, read `references/tax_codes.md`".
* **Why?** This keeps the initial context load low (~500 tokens) and only pays the "token cost" for deep knowledge if the user actually asks for it.

## 6. The Script Boundary: When to Code vs. When to Prompt
LLMs are probabilistic (good at reasoning); Scripts are deterministic (good at math/logic). Know the boundary.

### ⚠️ Critical Rule: Don't Script What LLMs Excel At

**Before creating a script, ask:** "Is this task primarily analysis, synthesis, or pattern recognition?"

If YES → Use LLM-driven workflow with structured guidance (checklists, decision trees)  
If NO → Use scripts for deterministic operations

### **Use LLMs (SKILL.md with checklists/references) for:**
* **Analysis tasks:**
    * Repository analysis (reading docs, detecting patterns, inferring conventions)
    * Code review (identifying issues, suggesting improvements)
    * Documentation synthesis (combining info from multiple sources)
    * Architecture discovery (understanding system structure)
* **Decision making:**
    * Choosing appropriate patterns based on context
    * Determining which tool/framework to use
    * Inferring user intent from vague queries
* **Pattern recognition:**
    * Detecting coding styles from samples
    * Identifying tech stacks from config files
    * Finding architectural patterns in code

**How to structure:** Provide detailed checklists, decision trees, and reference docs. Let the LLM work through them systematically.

**Example:** ✅ Repository analysis via `analysis_checklist.md` (not `analyze_repo.py` script)

### **Use Scripts (scripts/) for:**
* **Deterministic operations:**
    * Math and aggregation ("Calculate average revenue")
    * Precise transformations (PDF to text, image resizing)
    * File validation (schema checking, format verification)
* **External interactions:**
    * API calls with authentication
    * Database queries
    * File system operations requiring atomicity
* **Repetitive generation:**
    * Boilerplate code creation
    * Template rendering with many variables
    * Bulk operations on datasets

**How to structure:** Create focused scripts with clear inputs/outputs, error handling, and helpful messages.

**Example:** ✅ `scripts/validate_schema.py` for precise JSON schema validation

### Real-World Example: Repository Instructions Generator

**❌ Wrong approach:**
```python
# scripts/analyze_repo.py - trying to script what LLMs excel at
def detect_tech_stack():
    # Hardcoded patterns for each framework
    # Rigid logic that needs constant updates
    # Can't handle edge cases or new frameworks
```

**✅ Correct approach:**
```markdown
# references/analysis_checklist.md - structured guidance for LLM
## Phase 2: Tech Stack Detection
- [ ] Check package.json for: React, Vue, Angular, Express...
- [ ] Check requirements.txt for: Django, Flask, FastAPI...
- [ ] Check go.mod for Go frameworks
- [ ] Synthesize findings and infer primary stack
```

**Why correct?** LLMs can:
- Read any file format
- Recognize new frameworks not in the checklist
- Make inferences ("uses React hooks → modern React app")
- Adapt to unexpected patterns

**Why wrong?** Scripts:
- Need constant updates for new frameworks
- Can't handle edge cases (custom build systems)
- Can't synthesize information from multiple sources
- Can't make contextual inferences

### Decision Flowchart

```
┌─────────────────────────────────┐
│ Does this task require...      │
└─────────────────────────────────┘
           │
           ▼
    ┌─────────────┐
    │ Reading and │ YES → Use LLM with checklist
    │ synthesizing├─────────────────────────────────┐
    │ information?│                                 │
    └──────┬──────┘                                 │
           │ NO                                     │
           ▼                                        │
    ┌─────────────┐                                 │
    │ Pattern     │ YES → Use LLM with examples     │
    │ recognition ├─────────────────────────────────┤
    │ or inference│                                 │
    └──────┬──────┘                                 │
           │ NO                                     │
           ▼                                        │
    ┌─────────────┐                                 │
    │ Precise     │ YES → Use script                │
    │ calculation ├───────────────────┐             │
    │ or math?    │                   │             │
    └──────┬──────┘                   │             │
           │ NO                       │             │
           ▼                          │             │
    ┌─────────────┐                   │             │
    │ External    │ YES → Use script  │             │
    │ API or DB?  ├───────────────────┤             │
    └──────┬──────┘                   │             │
           │ NO                       │             │
           ▼                          │             │
    Use LLM (default)                 │             │
           │                          │             │
           └──────────────────────────┴─────────────┘
                          │
                          ▼
               Review decision with user
```

## 7. Common Anti-Patterns (What to Avoid)
* **The Analysis Script:** Creating scripts for tasks LLMs excel at (analysis, synthesis, pattern recognition).
    * *Example:* `analyze_repo.py` that tries to detect tech stacks with hardcoded patterns.
    * *Fix:* Use LLM-driven analysis with structured checklists in `references/`.
    * *Why:* LLMs can handle edge cases, new frameworks, and contextual inference better than rigid scripts.
* **The Code Wall:** Placing 50+ lines of Python code inside `SKILL.md` and asking the agent to "copy and run" it.
    * *Fix:* Move code to `scripts/my_script.py` and command "Run `scripts/my_script.py`".
* **The API Dump:** Pasting entire API documentation into the main instruction file.
    * *Fix:* Move to `references/api_docs.md`.
* **Silent Failures:** Scripts that fail without output.
    * *Fix:* Scripts must print clear errors to `stderr` so the agent can self-correct.
* **Hallucinated Paths:** Referring to files that don't exist.
    * *Fix:* Validate all paths are relative to the skill root (e.g., `references/data.csv`).
* **Asking for What You Can Infer:** Requesting user input for information already available in the codebase.
    * *Example:* Asking "What build tool do you use?" when `Makefile` exists.
    * *Fix:* Analyze first, ask only if information truly cannot be determined.

## 8. Security Guidelines
* **Sandboxing:** Always assume scripts run in a restricted environment. Do not rely on persistent state between runs.
* **Allowlisting:** Use the `allowed-tools` metadata field to restrict binary access. A text processing skill does not need `curl` access.
* **Read-Only References:** Treat `references/` as read-only to prevent prompt injection attacks where a file overrides system instructions.

## 9. Technical Constraints & Validation

### YAML Frontmatter
* **Name:** Max 64 chars, lowercase/numbers/hyphens only. No reserved words (`anthropic`, `claude`).
* **Description:** Max 1024 chars. Must be specific.

### Token Budgets
* **Instruction Limit:** Keep `SKILL.md` under 500 lines.
* **Separation:** If > 500 lines, split into `references/` files.
* **Efficiency:** Reference files are only loaded when explicitly read by the agent.

### Quality Checklist
* [ ] **Description:** Uses third-person ("Extracts data...") and includes triggers ("Use when...").
* [ ] **Testing:** Tested with multiple model classes (Haiku/Sonnet/Opus).
* [ ] **Paths:** Uses Unix-style forward slashes (`/`), never Windows backslashes (`\`).
* [ ] **Dependencies:** Explicitly lists required tools/packages; does not assume pre-installation.
* [ ] **Safety:** Scripts handle errors explicitly instead of failing silently.
