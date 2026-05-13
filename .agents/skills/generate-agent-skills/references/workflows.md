# Workflow Patterns for Agent Skills

This guide helps skill authors structure multi-step processes and decision logic within their skills.

---

## Sequential Workflows

For complex tasks, break operations into clear, sequential steps. Give the agent an overview of the process towards the beginning of SKILL.md:

**Example: Multi-step document processing**

```markdown
## Workflow

Processing a legal document involves these steps:

1. **Analyze** the document structure (identify sections, headers, metadata)
2. **Extract** relevant clauses (run `scripts/extract_clauses.py`)
3. **Validate** completeness (check against `references/required_clauses.md`)
4. **Generate** summary report (use template from `assets/report_template.md`)
5. **Output** results (save to user-specified location)

Follow these steps in order. If any step fails, stop and report the error.
```

**Key principles:**
- Number each step clearly
- Use action verbs (Analyze, Extract, Validate)
- Reference scripts/references explicitly
- Define failure handling

---

## Conditional Workflows

For tasks with branching logic, guide the agent through decision points using clear conditionals:

**Example: Content modification workflow**

```markdown
## Workflow Decision Tree

1. **Determine the modification type:**
   - **Creating new content?** → Follow "Creation Workflow" (§2)
   - **Editing existing content?** → Follow "Editing Workflow" (§3)
   - **Deleting/removing content?** → Follow "Deletion Workflow" (§4)

2. **Creation Workflow:**
   1. Validate required fields are present
   2. Run `scripts/create.py --template [TYPE]`
   3. Review output with user

3. **Editing Workflow:**
   1. Load existing content with `scripts/load.py`
   2. Apply modifications (preserve formatting)
   3. Validate changes against schema
   4. Save with `scripts/save.py --backup`

4. **Deletion Workflow:**
   1. Confirm user intent (ask explicitly)
   2. Create backup with `scripts/backup.py`
   3. Execute deletion
   4. Log operation
```

**Key principles:**
- Use visual hierarchy (bold questions, indented answers)
- Provide section references (§2, §3) for navigation
- Make decision criteria explicit
- Always include safety checks (confirmations, backups)

---

## Iterative Workflows

For tasks requiring refinement loops (code review, optimization, quality checks):

**Example: Code optimization workflow**

```markdown
## Optimization Loop

1. **Baseline Analysis:**
   - Run performance profiler
   - Identify bottlenecks (top 3 slowest operations)

2. **Optimization Iteration:**
   - Apply optimization technique (vectorization, caching, algorithm change)
   - Re-run profiler
   - **If performance improved by >10%:** Continue to next bottleneck
   - **If performance degraded or <10% improvement:** Revert change
   - **If all bottlenecks addressed:** Proceed to validation

3. **Validation:**
   - Run test suite (`scripts/run_tests.sh`)
   - Confirm results match baseline
   - Document optimizations applied

**Maximum iterations:** 5 (prevent infinite loops)
```

**Key principles:**
- Define clear loop exit conditions
- Set maximum iteration limits
- Include validation/testing in each cycle
- Document what changed

---

## Parallel Workflows

For tasks with independent sub-tasks that can run concurrently:

**Example: Multi-source data aggregation**

```markdown
## Parallel Data Collection

Execute these tasks independently (order doesn't matter):

### Task A: API Data Collection
1. Fetch from API endpoint (`scripts/fetch_api.py`)
2. Save to `data/api_results.json`

### Task B: Database Query
1. Run SQL query (`scripts/query_db.py`)
2. Save to `data/db_results.json`

### Task C: File Parsing
1. Parse CSV files in `input/` directory
2. Save to `data/csv_results.json`

### Aggregation (requires all tasks complete)
1. Merge all JSON files with `scripts/merge_data.py`
2. Apply deduplication logic
3. Generate final report
```

**Key principles:**
- Clearly mark tasks as independent
- Specify aggregation/merge point
- Define output locations for each task
- Handle partial failures gracefully

---

## Error Handling Patterns

### Pattern 1: Fail Fast

```markdown
## Validation

1. Check prerequisites:
   - Required files exist in `input/` directory
   - Python dependencies installed (`pip list | grep pandas`)
   - **If any check fails:** STOP. Report specific missing requirement.

2. Proceed with workflow only if all checks pass.
```

### Pattern 2: Graceful Degradation

```markdown
## Data Processing

1. **Primary method:** Use ML model for classification
   - If model file missing: Log warning and fall back to rule-based method
   
2. **Fallback method:** Use regex patterns from `references/patterns.md`
   - If patterns file missing: Use default heuristics
   
3. Always indicate which method was used in output
```

### Pattern 3: Retry with Backoff

```markdown
## API Interaction

1. Call API endpoint
2. **If HTTP 429 (rate limit):**
   - Wait 60 seconds
   - Retry (max 3 attempts)
   - If still failing: Abort and report
3. **If HTTP 500 (server error):**
   - Report immediately (don't retry)
```

---

## Best Practices

### ✅ DO:
- Use clear section headers for each workflow stage
- Provide explicit decision criteria (not vague "if needed")
- Reference specific scripts/files by path
- Define what success/failure looks like
- Set timeouts/iteration limits for loops
- Include user confirmation for destructive actions

### ❌ DON'T:
- Use ambiguous language ("maybe do X", "consider Y")
- Create circular dependencies between steps
- Assume the agent remembers previous steps
- Skip error handling
- Create workflows with no clear exit conditions
- Mix multiple workflow patterns without clear structure

---

## Workflow Selection Guide

| Workflow Type | Best For | Code Example | Docs Example | Data Example |
|---------------|----------|--------------|--------------|--------------|
| **Sequential** | Linear processes with dependencies | Deployment pipeline | Meeting notes generation | Data validation pipeline |
| **Conditional** | Tasks with multiple paths | Format conversion | Doc type selection | Schema migration routing |
| **Iterative** | Refinement and optimization | Code optimization | Content review cycles | Data quality improvement |
| **Parallel** | Independent sub-tasks | Multi-module builds | Multi-doc indexing | Multi-source data collection |

Choose the pattern that best matches your task's structure. Complex skills may combine patterns (e.g., sequential main flow with conditional branches).

---

## Domain-Specific Workflow Examples

### Documentation Repository Workflows

#### Sequential: Meeting Notes Generation
```markdown
## Workflow

Generating meeting notes from transcript:

1. **Load Context** - Read meeting agenda, previous notes
2. **Analyze Transcript** - Identify speakers, topics, decisions
3. **Extract Action Items** - Find assignments, deadlines, owners
4. **Structure Content** - Apply team's note template
5. **Generate Summary** - Create executive summary
6. **Save and Notify** - Write to docs/ and notify attendees

Follow steps in order. If transcript quality is poor, stop at step 2 and request clarification.
```

#### Conditional: Documentation Update
```markdown
## Workflow Decision Tree

1. **Determine update type:**
   - **New page?** → Use creation workflow (§2)
   - **Edit existing?** → Use update workflow (§3)
   - **Deprecating?** → Use deprecation workflow (§4)

2. **Creation Workflow:**
   1. Check if topic already covered elsewhere
   2. Select appropriate template from `assets/templates/`
   3. Generate initial content
   4. Add to navigation index
   5. Create cross-references

3. **Update Workflow:**
   1. Load existing content
   2. Preserve version history
   3. Apply changes
   4. Update "Last Modified" timestamp
   5. Regenerate related docs if needed

4. **Deprecation Workflow:**
   1. Add deprecation notice at top
   2. Link to replacement documentation
   3. Remove from navigation
   4. Keep for historical reference
```

#### Iterative: Content Review Cycles
```markdown
## Review Loop

1. **Initial Draft:**
   - Generate content based on requirements
   - Apply writing style from `references/style_guide.md`

2. **Quality Check Iteration:**
   - Run readability analysis
   - Check against style guide
   - **If quality score < 80%:** Revise and re-check
   - **If quality score ≥ 80%:** Proceed to validation

3. **Technical Validation:**
   - Verify all links work
   - Check code examples compile
   - Validate screenshots are current
   - **If validation fails:** Fix issues and return to step 2
   - **If validation passes:** Mark ready for review

**Maximum iterations:** 3 (prevent infinite loops)
```

### Data Repository Workflows

#### Sequential: Data Quality Pipeline
```markdown
## Workflow

Data validation process:

1. **Load Rules** - Read quality rules from `references/quality_standards.md`
2. **Schema Validation** - Check data matches expected schema
3. **Completeness Check** - Verify no missing required fields
4. **Range Validation** - Ensure values within acceptable ranges
5. **Relationship Check** - Validate foreign key relationships
6. **Generate Report** - Create quality report with issues found
7. **Apply Fixes** - Auto-fix issues where possible
8. **Human Review** - Flag complex issues for manual review

Execute in order. If schema validation fails, stop immediately.
```

#### Conditional: Schema Migration
```markdown
## Migration Decision Tree

1. **Analyze change type:**
   - **Breaking change?** → Use breaking migration workflow (§2)
   - **Backwards-compatible?** → Use compatible migration workflow (§3)
   - **Data transformation needed?** → Use transformation workflow (§4)

2. **Breaking Migration:**
   1. Create migration script in `scripts/migrations/`
   2. Generate rollback script
   3. Test on sample data
   4. Create migration documentation
   5. Require manual approval before execution

3. **Compatible Migration:**
   1. Apply schema changes automatically
   2. Backfill new fields with defaults
   3. Validate data integrity
   4. Update documentation

4. **Transformation Workflow:**
   1. Analyze old and new schemas
   2. Generate transformation rules
   3. Test transformation on subset
   4. Execute transformation with progress tracking
   5. Validate all records transformed correctly
```

---

## Cross-Domain Pattern: Repository Analysis

**Works for code, docs, and data repos - just adapt the discovery targets:**

### Code Repository Analysis
```markdown
1. **Documentation Discovery** → README, CONTRIBUTING
2. **Tech Stack Detection** → package.json, requirements.txt
3. **Build System** → Makefile, CI configs
4. **Code Patterns** → Sample source files
```

### Docs Repository Analysis
```markdown
1. **Structure Discovery** → README, doc index
2. **Generator Detection** → mkdocs.yml, conf.py
3. **Build System** → Doc build commands
4. **Writing Patterns** → Sample doc files
```

### Data Repository Analysis
```markdown
1. **Documentation Discovery** → README, data dictionary
2. **Schema Detection** → schema files, DDL
3. **Pipeline Discovery** → ETL configs, DAGs
4. **Data Patterns** → Sample data records
```

**Key insight:** Same workflow pattern (sequential discovery), different targets!
