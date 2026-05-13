---
name: generate-agent-skills
description: Architects, generates, and validates Agent Skills. Enforces specification and best practices. Used any time an agent skill must be created or updated.
compatibility: python3
allowed-tools: python3 ls grep cat mkdir
metadata:
  author: canonical/platform-engineering
  version: "1.0.0"
---

# Agent Skill Architect Workflow

This skill guides you through creating high-quality Agent Skills following a proven 6-step process.

---

## 🚨 **CRITICAL WORKFLOW REQUIREMENTS** 🚨

**Before you begin, understand these NON-NEGOTIABLE rules:**

1. **You MUST run `scripts/scaffold_skill.py` in Step 3.**  
   Manual file creation is PROHIBITED. The scaffolding script ensures consistency.

2. **You MUST use the generated templates.**  
   After scaffolding, templates exist in `references/`. Use them as your foundation.

3. **You MUST run `scripts/validate_skill.py` in Step 5.**  
   Validation catches errors before they propagate.

4. **You MUST follow all 6 steps in order.**  
   Skipping steps leads to non-compliant or broken skills.

**If you bypass scaffolding scripts, you have FAILED this workflow.**

---

## Step 1: Understanding the Skill

**Before scaffolding**, clearly understand how the skill will be used through concrete examples.

### For New Skills:
Ask the user clarifying questions to understand:
- What functionality should the skill support?
- What are example queries that should trigger this skill?
- What outputs or actions should result?
- Are there existing workflows or tools to integrate?

**Example questions:**
- "Can you give some examples of how this skill would be used?"
- "What would a user say that should trigger this skill?"
- "What existing scripts or documentation should be included?"

### For Existing Skills:
If working with an existing skill, analyze:
- Current SKILL.md structure and content
- Existing scripts, references, and assets
- What's working well vs. what needs improvement

**Conclude this step when:** You have a clear sense of the skill's functionality and triggering scenarios.

---

## Step 2: Planning Reusable Contents

Analyze the concrete examples from Step 1 to identify what **reusable resources** would help.

### ⚠️ Critical Decision: Script vs. Checklist

**Before planning scripts, ask:** "Is this task primarily analysis or computation?"

**Analysis tasks** (reading, synthesizing, pattern recognition):
→ Use **checklists** or **reference docs** for LLM to follow
→ Examples: Repository analysis, code review, documentation synthesis

**Computation tasks** (math, APIs, precise transformations):
→ Use **scripts** for deterministic execution
→ Examples: Schema validation, API calls, file format conversion

**Real example from this session:**
- ❌ Initially planned `analyze_repo.py` script
- ✅ Corrected to `analysis_checklist.md` reference
- **Why:** Repository analysis = LLM strength (reading, pattern detection, synthesis)

**See `references/BEST_PRACTICES.md` §6 for detailed decision flowchart.**

---

### Ask for each example:
1. How would I execute this task from scratch?
2. What scripts, references, or assets would make this repeatable?
3. **Is this analysis (LLM) or computation (script)?**

### Resource Types:

**scripts/** - For deterministic operations only:
- ✅ Math/computation (calculations, aggregations)
- ✅ External interactions (API calls, database queries)
- ✅ Precise transformations (file format conversion, schema validation)
- ✅ Repetitive generation (boilerplate rendering)
- ❌ Analysis tasks (use checklists instead)
- ❌ Pattern recognition (LLM excels at this)

**references/** - For LLM-driven analysis and knowledge:
- ✅ Checklists for systematic analysis (e.g., repository discovery)
- ✅ Pattern libraries (e.g., positive constraint conversions)
- ✅ API documentation (endpoints, parameters)
- ✅ Domain knowledge (company policies, industry standards)
- ✅ Decision trees and workflows

**assets/** - For files used in output:
- ✅ Templates (documents, slides, boilerplate code)
- ✅ Images (logos, icons, diagrams)
- ✅ Fonts (typography files)
- ✅ Seed data (sample datasets, fixtures)

**Output:** A list of specific files to create with correct categorization (script vs reference)

---

## Step 3: Skill Scaffolding

**⚠️ MANDATORY STEP - DO NOT SKIP ⚠️**

You MUST execute the scaffolding script. Manual file creation is PROHIBITED.

### Command:
```bash
python3 scripts/scaffold_skill.py --name <skill-name>
```

### Options:
- **Default mode:** Creates SKILL.md + example files in scripts/, references/, assets/
- **Simple mode:** Use `--simple` flag for minimal structure (SKILL.md only)

**The script will:**
- ✅ Validate naming conventions (lowercase, hyphens, alphanumeric)
- ✅ Create skill directory in .github/skills/
- ✅ Generate SKILL.md with structuring guidance
- ✅ Create example files to demonstrate resource organization

**Note:** The script auto-detects `.github/skills` from git root. Naming must match regex: `^[a-z0-9][a-z0-9-]*[a-z0-9]$`

---

### ✅ **Verification Checkpoint**

After running the scaffolding script, confirm these files exist:
```bash
ls -la .github/skills/<skill-name>/
```

**Expected output:**
- `SKILL.md` (with "Structuring This Skill" guidance section)
- `scripts/example.py` (placeholder script)
- `references/example_reference.md` (placeholder reference)
- `assets/README.md` (if using default mode)

**🛑 STOP CONDITIONS:**
- If `SKILL.md` does NOT exist → Scaffolding failed, do NOT proceed
- If you created files manually → You have violated the workflow, DELETE and re-run script
- If the script reported errors → Fix errors before proceeding to Step 4

---

## Step 4: Content Generation

Populate the skill with actual content.

### 4.1: Implement Reusable Resources First

Start with scripts/, references/, and assets/ identified in Step 2.

**For scripts:**
- Replace `scripts/example.py` with actual implementation
- Test by running: `python3 scripts/<script_name>.py`
- Ensure error messages are descriptive (print to stderr)

**For references:**
- Replace `references/example_reference.md` with actual docs
- Keep SKILL.md lean - move details here
- For large files (>100 lines), add Table of Contents

**For assets:**
- Add actual template files, images, fonts
- Replace or delete `assets/README.md`
- Use descriptive filenames

**Important:** Delete any example files you don't need!

### 4.2: Write SKILL.md Content

Follow the structuring guidance embedded in the generated SKILL.md template.

**Choose your structure pattern:**
- **Workflow-Based:** Sequential processes (see `references/workflows.md`)
- **Task-Based:** Tool collections with different operations
- **Reference/Guidelines:** Standards, specifications, coding rules
- **Capabilities-Based:** Integrated systems with multiple features

**Key elements:**

1. **Frontmatter (YAML):**
   - `name`: Must match directory name exactly
   - `description`: High-entropy, keyword-rich, 3rd person
     - Include WHEN to use this skill (triggers)
     - Include WHAT the skill does (capabilities)
     - Example: "Processes PDF documents for form filling, text extraction, and merging. Use when working with PDF files or when user requests document manipulation tasks."

2. **Body (Markdown):**
   - Use imperative/infinitive form ("Run the script", not "You should run")
   - Reference scripts/references explicitly by path
   - Consult `references/BEST_PRACTICES.md` for the "Freedom Scale"
   - Consult `references/output-patterns.md` for output formatting

**Delete the "Structuring This Skill" section** when done - it's guidance only!

### 4.3: Design Patterns

**For multi-step processes:** See `references/workflows.md`
- Sequential workflows (step 1 → step 2 → step 3)
- Conditional workflows (if/then branching)
- Iterative workflows (refinement loops)

**For consistent outputs:** See `references/output-patterns.md`
- Strict templates (non-negotiable formats)
- Flexible guidance (adaptable structure)
- Examples-based (show don't tell)
- Validation checklists (quality requirements)

---

## Step 5: Validation

**⚠️ MANDATORY STEP - DO NOT SKIP ⚠️**

Run the validation script to ensure specification compliance.

### Command:
```bash
python3 scripts/validate_skill.py --path <path-to-skill-root>
```

**Example:**
```bash
python3 scripts/validate_skill.py --path .github/skills/diagnose-ci-failure
```

### What it checks:
- ✅ Directory naming regex (`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)
- ✅ SKILL.md exists
- ✅ YAML frontmatter has required fields (name, description)
- ✅ Name in YAML matches directory name
- ⚠️  Advisory: Presence of references/ and scripts/ (warnings only)

**If validation fails:**
- Read the error output carefully
- Fix critical violations immediately
- Warnings are informational (acceptable for simple skills)

**When valid:** Proceed to testing!

---

### ✅ **Post-Validation Checklist**

Before proceeding to Step 6, confirm:

**Workflow Compliance:**
- [ ] I RAN `scripts/scaffold_skill.py` (Step 3)
- [ ] I USED the generated templates from scaffolding
- [ ] I CONSULTED `references/TEMPLATES.md` and `references/BEST_PRACTICES.md` (Step 4)
- [ ] I RAN `scripts/validate_skill.py` (Step 5)
- [ ] Validation script reported SUCCESS (no critical errors)

**Content Quality:**
- [ ] YAML frontmatter includes `name` and `description`
- [ ] Description is high-entropy and keyword-rich
- [ ] No "Structuring This Skill" guidance section remains in SKILL.md
- [ ] Example files (`example.py`, `example_reference.md`) are deleted or replaced
- [ ] Scripts are in `scripts/`, references in `references/`, templates in `assets/`

**🛑 STOP CONDITION:**
If you did NOT run the scaffolding script or manually created files, STOP and re-do from Step 3.

---

## Step 6: Testing and Iteration

After creating the skill, test and refine based on real usage.

### Testing Workflow:

1. **Test with real examples** from Step 1
   - Does the skill trigger on expected queries?
   - Do scripts execute without errors?
   - Is output quality acceptable?

2. **Identify friction points:**
   - Are instructions clear enough?
   - Are there missing scripts or references?
   - Is context loaded efficiently?

3. **Iterate on improvements:**
   - Update SKILL.md for clarity
   - Add missing examples or edge cases
   - Optimize script error handling
   - Split large references if needed (progressive disclosure)

4. **Re-validate** after changes

### Common Iteration Patterns:

**Problem:** Skill isn't triggering when expected
**Solution:** Enhance description with more keywords and trigger scenarios

**Problem:** Agent struggles with workflow steps
**Solution:** Add decision tree or flowchart; consult `references/workflows.md`

**Problem:** Context feels bloated
**Solution:** Move content from SKILL.md to references/; add grep hints

**Problem:** Scripts fail in edge cases
**Solution:** Add error handling; print descriptive messages to stderr

**Problem:** Output quality inconsistent
**Solution:** Add templates or validation checklist; see `references/output-patterns.md`

### When to Stop Iterating:

✅ Skill triggers reliably on target queries  
✅ Workflows execute without confusion  
✅ Output quality meets requirements  
✅ No critical errors in testing  

---

## Knowledge Retrieval

If questions arise during skill creation:

**Specification questions** (naming, structure, required files):
→ Read `references/SPECIFICATION.md`

**Best practices** (context economy, freedom scale, anti-patterns):
→ Read `references/BEST_PRACTICES.md`

**Templates and examples** (frontmatter, structure patterns):
→ Read `references/TEMPLATES.md`

**Workflow design** (sequential, conditional, iterative):
→ Read `references/workflows.md`

**Output formatting** (templates, examples, validation):
→ Read `references/output-patterns.md`

**Do not hallucinate answers.** Always consult the authoritative sources.
