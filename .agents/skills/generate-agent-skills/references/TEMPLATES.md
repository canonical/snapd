# Agent Skill Templates

Use these templates to quickly scaffold new skills that adhere to the Agent Skill Specification.

---

## Choosing a Template

Select based on your skill's primary purpose:

| Template | Best For | Complexity | Resources Needed |
|----------|----------|------------|------------------|
| **Type A: Pure Prompt** | Reasoning, analysis, writing | Low | SKILL.md only |
| **Type B: Tool-Backed** | Data processing, APIs, file ops | Medium | SKILL.md + scripts/ |
| **Type C: Knowledge-Heavy** | Domain expertise, standards | Medium | SKILL.md + references/ |
| **Type D: Asset-Driven** | Template generation, boilerplate | Medium-High | SKILL.md + assets/ |

---

## Template 1: Pure Prompt Skill (Type A)

*Use for reasoning, text generation, and analysis tasks that don't require scripts or external knowledge.*

**Examples:** Code review comments, commit message generation, text summarization

```markdown
---
name: [skill-name]
description: [Third-person description of capabilities. Include WHEN to trigger (specific scenarios, keywords). Max 1024 chars.]
---

# [Skill Title]

## Overview

[1-2 sentences explaining what this skill does and its primary use case]

## Workflow

1. **Analyze** the input: [INPUT_VARIABLE]
   - Identify key characteristics
   - Check for [specific criteria]

2. **Process** using [approach/method]:
   - Apply [technique/pattern]
   - Consider [constraints/requirements]

3. **Generate output** following this format:

[Define output structure - see references/output-patterns.md for patterns]

## Constraints

* [Constraint 1: e.g., Word count limit, tone requirements]
* [Constraint 2: e.g., Must use provided data only]
* [Constraint 3: e.g., Follow specific formatting rules]

## Examples

**Input:** [Realistic example input]

**Output:**
\`\`\`
[Show expected output]
\`\`\`

**Input:** [Edge case example]

**Output:**
\`\`\`
[Show how edge case is handled]
\`\`\`
```

---

## Template 2: Tool-Backed Skill (Type B)

*Use for data processing, external APIs, file manipulation, and deterministic operations.*

**Examples:** PDF processing, API integration, data transformation

```markdown
---
name: [skill-name]
description: [Third-person description. Explicitly state when to trigger this skill - file types, operations, keywords.]
compatibility: python3
allowed-tools: python3 grep cat
---

# [Skill Title]

## Overview

[1-2 sentences explaining capabilities and when to use this skill]

## Quick Start

For simple cases:
\`\`\`bash
python3 scripts/[primary_script].py --input [FILE] --output [RESULT]
\`\`\`

## Workflow

### 1. Input Validation

Ensure the user has provided:
- [Required parameter 1]
- [Required parameter 2]

**If missing:** Stop and ask for the required information.

### 2. Execution Strategy

**Do not compute this manually.** Execute the appropriate script:

**For [use case 1]:**
\`\`\`bash
python3 scripts/[script1].py --arg [VALUE]
\`\`\`

**For [use case 2]:**
\`\`\`bash
python3 scripts/[script2].py --mode [MODE] --arg [VALUE]
\`\`\`

### 3. Error Handling

* **If script fails:** Report the error exactly as printed in stderr
* **If output is empty:** Verify input file exists and is readable
* **If [specific error]:** [Specific solution]

**Do not attempt to fix script code or retry blindly.**

### 4. Output Validation

After execution:
- Verify output file was created
- Check file size is > 0 bytes
- Report success and output location to user

## Reference

See `references/[schema_or_spec].md` for:
- Detailed API specifications
- Input/output format definitions
- Advanced usage examples
```

---

## Template 3: Knowledge-Heavy Skill (Type C)

*Use for domain expertise, company standards, industry regulations, complex specifications.*

**Examples:** Security audit guidelines, brand style compliance, legal document review

```markdown
---
name: [skill-name]
description: [Third-person description. Include domain keywords, document types, compliance standards that trigger this skill.]
---

# [Skill Title]

## Overview

[1-2 sentences explaining the domain and skill purpose]

## Core Principles

This skill enforces [standards/guidelines/regulations]:

1. **[Principle 1]:** [Brief explanation]
2. **[Principle 2]:** [Brief explanation]
3. **[Principle 3]:** [Brief explanation]

## Workflow

### 1. Domain Assessment

Determine which area applies:
- **[Domain A]:** See `references/[domain_a].md` for rules
- **[Domain B]:** See `references/[domain_b].md` for rules
- **[Domain C]:** See `references/[domain_c].md` for rules

### 2. Apply Standards

Load the relevant reference:
\`\`\`bash
cat references/[applicable_domain].md
\`\`\`

Follow the guidelines strictly for:
- [Aspect 1]: [Where to find details]
- [Aspect 2]: [Where to find details]

### 3. Validation Checklist

Verify compliance with:
- [ ] [Requirement 1]
- [ ] [Requirement 2]
- [ ] [Requirement 3]

**If any checks fail:** Document the violation and suggest remediation.

## References

This skill includes detailed domain knowledge:

### references/[domain_a].md
[Brief description of what's covered]

**When to read:** [Triggering condition]

### references/[domain_b].md
[Brief description of what's covered]

**When to read:** [Triggering condition]

## Tips

For large reference files, use grep:
\`\`\`bash
grep "[search_term]" references/[file].md
\`\`\`
```

---

## Template 4: Asset-Driven Skill (Type D)

*Use for generating documents, code boilerplate, or outputs based on templates.*

**Examples:** Report generation, project scaffolding, document creation

```markdown
---
name: [skill-name]
description: [Third-person description. Mention output types (documents, code), template types, when to use.]
---

# [Skill Title]

## Overview

[1-2 sentences explaining what gets generated and from what inputs]

## Workflow

### 1. Template Selection

Choose the appropriate template based on user needs:

**For [use case 1]:** Use `assets/[template1].[ext]`
**For [use case 2]:** Use `assets/[template2].[ext]`
**For [use case 3]:** Use `assets/[template3].[ext]`

### 2. Gather Required Information

Collect from user:
- [Field 1]: [Description]
- [Field 2]: [Description]
- [Field 3]: [Description]

### 3. Populate Template

1. Copy template from assets/
2. Replace placeholders:
   - `{{PLACEHOLDER1}}` → [Value from user]
   - `{{PLACEHOLDER2}}` → [Value from user]
3. Apply formatting rules from `references/formatting.md` (if applicable)

### 4. Generate Output

Create final file:
\`\`\`bash
cp assets/[template].[ext] output/[filename].[ext]
# Then modify with user data
\`\`\`

### 5. Validation

Ensure output meets requirements:
- [ ] All placeholders replaced
- [ ] Formatting is correct
- [ ] Required sections present
- [ ] File is valid [format type]

## Available Templates

### assets/[template1].[ext]
**Purpose:** [What this template is for]
**Placeholders:** `{{FIELD1}}`, `{{FIELD2}}`, `{{FIELD3}}`
**Output:** [What gets generated]

### assets/[template2].[ext]
**Purpose:** [What this template is for]
**Placeholders:** `{{FIELD_A}}`, `{{FIELD_B}}`
**Output:** [What gets generated]

## Examples

**User Request:** [Realistic example]

**Template Used:** `assets/[template1].[ext]`

**Generated Output:**
\`\`\`
[Show example of populated template]
\`\`\`
```

---

## Advanced: Hybrid Template

*Combine multiple resource types for complex skills.*

**Example:** Full-featured document processing skill with scripts, references, and templates

```markdown
---
name: [skill-name]
description: [Comprehensive description covering all capabilities - processing, validation, generation. Include all trigger keywords.]
compatibility: python3
allowed-tools: python3 bash grep cat
---

# [Skill Title]

## Overview

[2-3 sentences covering the full scope of capabilities]

## Workflow Decision Tree

Determine the user's goal:

1. **Processing existing [documents]?** → See §2 Processing Workflow
2. **Validating [documents]?** → See §3 Validation Workflow
3. **Creating new [documents]?** → See §4 Generation Workflow

---

## §2 Processing Workflow

[Follow Template B pattern - tool-backed with scripts]

---

## §3 Validation Workflow

[Follow Template C pattern - knowledge-heavy with references]

---

## §4 Generation Workflow

[Follow Template D pattern - asset-driven with templates]

---

## Resources Overview

### scripts/
- `process.py` - Processing operations
- `validate.py` - Validation checks
- `generate.py` - Document generation

### references/
- `standards.md` - Compliance requirements
- `schemas.md` - Data structure definitions
- `examples.md` - Common patterns

### assets/
- `template_a.[ext]` - [Purpose]
- `template_b.[ext]` - [Purpose]
```

---

## Best Practices

### ✅ DO:
- Keep SKILL.md under 500 lines (move content to references/ if needed)
- Use imperative voice ("Run the script" not "You should run")
- Reference files by explicit path (`scripts/process.py`)
- Include concrete examples
- Define clear success/failure criteria
- Provide grep hints for large reference files

### ❌ DON'T:
- Duplicate content between SKILL.md and references/
- Use vague instructions ("maybe do X", "consider Y")
- Embed large code blocks (move to scripts/)
- Embed large documentation (move to references/)
- Create workflows without error handling
- Forget to mention what assets exist

---

## Next Steps After Templating

1. **Replace all [placeholders]** with actual content
2. **Delete unused sections** (not every template needs every section)
3. **Test with real examples** to verify workflow clarity
4. **Consult references:**
   - `BEST_PRACTICES.md` for context economy and freedom scale
   - `workflows.md` for complex process patterns
   - `output-patterns.md` for output formatting
5. **Validate:** `python3 scripts/validate_skill.py --path [skill-path]`

---

## Domain-Specific Adaptations

The templates above are universal - they work across different repository types. Here's how to adapt them for your domain:

### Code Repositories

**Common patterns:**
- **Analysis tasks:** Tech stack detection, code pattern analysis, build system discovery
- **Generation tasks:** Test suite generation, API client creation, migration scripts
- **Validation tasks:** Code quality checks, security scanning, dependency audits

**Example skills:**
- `generate-tests` (Type B) - Generate unit tests from source code
- `analyze-dependencies` (Type C) - Audit dependency security and licenses
- `refactor-to-typescript` (Type B) - Migrate JavaScript to TypeScript

**Key references:**
- Code style guides
- Framework-specific patterns
- Build tool configurations

---

### Documentation Repositories

**Common patterns:**
- **Analysis tasks:** Doc structure discovery, writing style detection, taxonomy analysis
- **Generation tasks:** Meeting notes from transcripts, API docs from code, changelog generation
- **Transformation tasks:** Format conversion, content migration, index generation

**Example skills:**
- `generate-meeting-notes` (Type A) - Extract structured notes from meeting transcripts
  - **Workflow:** Analyze transcript → Extract action items → Apply template
  - **Resources:** `references/note_template.md`, `references/extraction_checklist.md`
  
- `migrate-docs-format` (Type D) - Convert Markdown to reStructuredText
  - **Workflow:** Analyze source → Select target template → Transform content
  - **Resources:** `assets/rst_templates/`, `scripts/validate_rst.py`
  
- `index-documentation` (Type C) - Generate searchable index from doc corpus
  - **Workflow:** Scan docs → Build taxonomy → Generate index structure
  - **Resources:** `references/taxonomy_rules.md`, `references/index_format.md`

**Key references:**
- Writing style guides
- Document structure templates
- Content taxonomy

**Adaptation tips:**
- Replace "code analysis" with "content analysis"
- Replace "tech stack detection" with "doc generator detection" (MkDocs, Sphinx, Docusaurus)
- Use same workflow patterns (sequential, conditional) for doc generation

---

### Data Repositories

**Common patterns:**
- **Analysis tasks:** Schema detection, data quality checks, pipeline discovery
- **Generation tasks:** Data validation reports, schema documentation, pipeline configs
- **Transformation tasks:** Data format conversion, schema migration, ETL generation

**Example skills:**
- `validate-data-quality` (Type B) - Check data against quality rules
  - **Workflow:** Load rules → Scan data → Generate quality report
  - **Resources:** `references/quality_rules.md`, `scripts/run_validation.py`
  
- `generate-schema-docs` (Type A) - Document data schemas automatically
  - **Workflow:** Analyze schema files → Extract metadata → Generate docs
  - **Resources:** `references/doc_template.md`
  
- `migrate-schema` (Type B) - Migrate data between schema versions
  - **Workflow:** Analyze old/new schemas → Generate migration → Validate
  - **Resources:** `scripts/migrate.py`, `references/migration_patterns.md`

**Key references:**
- Data quality standards
- Schema formats (JSON Schema, Avro, Protobuf)
- Pipeline configurations

**Adaptation tips:**
- Replace "code patterns" with "data patterns"
- Use validation checklists for data quality
- Scripts appropriate here (deterministic transformations)

---

### Mixed/Hybrid Repositories

**Example: Monorepo with code + docs + config**

Use **conditional workflows** to route based on file type:

```markdown
## Workflow Decision Tree

1. **Determine file type:**
   - **Code files (.ts, .py)?** → Apply code analysis workflow
   - **Docs files (.md)?** → Apply docs analysis workflow
   - **Config files (.yaml, .json)?** → Apply config validation workflow
```

---

## Template Selection Examples

### Code Repository Examples

| Goal | Template | Why |
|------|----------|-----|
| "Generate commit messages" | Type A (Pure Prompt) | No external tools needed, pure reasoning |
| "Process PDF forms" | Type B (Tool-Backed) | Requires PDF manipulation scripts |
| "Enforce brand guidelines" | Type C (Knowledge-Heavy) | Needs detailed standards documentation |
| "Create project boilerplate" | Type D (Asset-Driven) | Copies and modifies template files |

### Documentation Repository Examples

| Goal | Template | Why |
|------|----------|-----|
| "Generate meeting notes" | Type A (Pure Prompt) | Extract and structure from transcript |
| "Convert Markdown to RST" | Type D (Asset-Driven) | Uses RST templates + validation |
| "Enforce writing style" | Type C (Knowledge-Heavy) | Needs style guide reference |
| "Build doc index" | Type A (Pure Prompt) | LLM analyzes and structures taxonomy |

### Data Repository Examples

| Goal | Template | Why |
|------|----------|-----|
| "Validate data quality" | Type B (Tool-Backed) | Deterministic validation rules |
| "Document schemas" | Type A (Pure Prompt) | Analyze and describe schemas |
| "Enforce data standards" | Type C (Knowledge-Heavy) | Needs quality standards docs |
| "Generate ETL config" | Type D (Asset-Driven) | Uses pipeline templates |
| "Full document system" | Hybrid | Needs processing + validation + generation |