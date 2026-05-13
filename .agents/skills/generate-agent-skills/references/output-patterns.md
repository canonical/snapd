# Output Patterns for Agent Skills

This guide helps skill authors define clear output expectations for agents using their skills.

---

## When to Use Output Patterns

Use output patterns when:
- Results must follow a specific format (reports, code, documents)
- Consistency across multiple invocations is critical
- Quality standards need explicit definition
- Multiple output types are possible

**Don't over-specify:** If the task is creative or open-ended, rigid templates may harm quality.

---

## Pattern 1: Strict Template

Use when output format is non-negotiable (API responses, data formats, compliance documents).

**Example: Security Audit Report**

```markdown
## Output Format

**You must use this exact template structure:**

```
# Security Audit Report: [PROJECT_NAME]
**Date:** [YYYY-MM-DD]
**Auditor:** [NAME]

## Executive Summary
[1-2 paragraph overview of findings and risk level]

## Critical Vulnerabilities (Severity: HIGH)
- **[CVE-ID or Description]**
  - Location: [file:line]
  - Impact: [description]
  - Remediation: [specific fix]

## Medium Vulnerabilities (Severity: MEDIUM)
[Same structure as above]

## Low-Risk Issues (Severity: LOW)
[Same structure as above]

## Compliance Status
- [ ] OWASP Top 10 compliance
- [ ] Authentication best practices
- [ ] Data encryption standards

## Recommendations
1. [Prioritized action item]
2. [Prioritized action item]
```

**Do not deviate from this structure.** All sections are mandatory.
```

**Key elements:**
- Use "must" language for required sections
- Show complete template with placeholders
- Specify mandatory vs optional sections
- Use code blocks to preserve formatting

---

## Pattern 2: Flexible Guidance

Use when adaptation is beneficial but structure still matters (analysis, summaries, documentation).

**Example: Code Review Comments**

```markdown
## Output Format

Use this structure as a sensible default, but adapt based on the code being reviewed:

**For each issue found:**

1. **Location:** `filename.py:line_number`
2. **Severity:** [Critical | Important | Suggestion]
3. **Issue:** Brief description of the problem
4. **Why it matters:** Impact or risk explanation
5. **Suggested fix:** Specific recommendation

**Adjust sections as needed:**
- For simple typos: Just location + suggested fix
- For architectural concerns: Add "Broader implications" section
- For security issues: Add "Attack vector" section

**Example output:**

```
**Location:** `auth.py:45`
**Severity:** Critical
**Issue:** Password stored in plaintext
**Why it matters:** Credential theft if database compromised
**Attack vector:** SQL injection could expose all passwords
**Suggested fix:** Use bcrypt hashing: `bcrypt.hashpw(password, bcrypt.gensalt())`
```
```

**Key elements:**
- Provide default structure
- Explicitly allow adaptations
- Show examples of variations
- Explain when to add/remove sections

---

## Pattern 3: Examples-Based

Use when showing is clearer than telling (commit messages, naming conventions, code style).

**Example: Git Commit Messages**

```markdown
## Commit Message Format

Generate commit messages following these examples:

### Example 1: Feature Addition
**Input:** "Added user authentication with JWT tokens and refresh token rotation"

**Output:**
```
feat(auth): implement JWT-based authentication

- Add login endpoint with JWT token generation
- Implement refresh token rotation mechanism
- Add token validation middleware
- Include rate limiting for auth endpoints
```

### Example 2: Bug Fix
**Input:** "Fixed bug where dates displayed incorrectly in reports due to timezone handling"

**Output:**
```
fix(reports): correct date formatting in timezone conversion

Use UTC timestamps consistently across report generation.
Convert to user's local timezone only in the presentation layer.

Closes #234
```

### Example 3: Refactoring
**Input:** "Refactored database queries to use connection pooling"

**Output:**
```
refactor(db): migrate to connection pooling

- Replace direct connections with pool manager
- Add connection timeout configuration
- Improve query performance by ~40%
```

**Pattern to follow:**
- Type(scope): Brief description (50 chars max)
- Blank line
- Detailed explanation (what + why)
- Optional: Performance impact, issue references
```

**Key elements:**
- Multiple concrete examples
- Show input → output mapping
- Highlight pattern explicitly
- Cover edge cases with examples

---

## Pattern 4: Validation Checklist

Use when output quality depends on multiple criteria (code generation, document creation).

**Example: API Endpoint Generation**

```markdown
## Output Requirements

Your generated endpoint must pass this checklist:

### Functionality
- [ ] Implements specified HTTP method (GET/POST/PUT/DELETE)
- [ ] Validates all required parameters
- [ ] Returns appropriate status codes (200, 400, 404, 500)
- [ ] Handles authentication/authorization

### Code Quality
- [ ] Includes docstring with description and parameter types
- [ ] Uses type hints for all parameters and return values
- [ ] Error messages are descriptive and actionable
- [ ] No hardcoded credentials or secrets

### Testing
- [ ] Includes at least 3 unit tests (success, validation error, auth error)
- [ ] Tests use mocking for external dependencies
- [ ] All edge cases covered

### Documentation
- [ ] OpenAPI/Swagger spec entry added
- [ ] Example request/response included in docstring

**If any item fails, revise the output before presenting to the user.**
```

**Key elements:**
- Checkbox format for clarity
- Categorize criteria (functionality, quality, testing)
- Make criteria specific and testable
- State what happens if checks fail

---

## Pattern 5: Quality Tiers

Use when multiple quality levels are acceptable (quick drafts vs polished outputs).

**Example: Technical Documentation**

```markdown
## Output Quality Levels

Choose the appropriate level based on user's needs:

### Level 1: Quick Reference (5 min)
**When to use:** User needs immediate guidance

**Format:**
- Brief overview (2-3 sentences)
- Bulleted list of key points
- Code snippet with inline comments
- No styling or examples

### Level 2: Standard Documentation (20 min)
**When to use:** Default quality level

**Format:**
- Introduction with context
- Structured sections with headers
- Multiple code examples with explanations
- Common pitfalls and solutions
- Links to related documentation

### Level 3: Comprehensive Guide (60 min)
**When to use:** User requests detailed coverage or tutorial

**Format:**
- Full tutorial with step-by-step instructions
- Architecture diagrams or flowcharts
- Real-world use case examples
- Troubleshooting guide
- Performance considerations
- API reference table

**Ask the user if unsure which level to use.** Default to Level 2.
```

**Key elements:**
- Clear tier definitions
- Time estimates
- When to use each tier
- Specify default behavior

---

## Pattern 6: Hybrid (Template + Examples)

Combine strict structure with concrete examples for complex outputs.

**Example: Test Suite Generation**

```markdown
## Test Suite Structure

**Required structure:**

```python
import pytest
from unittest.mock import Mock, patch
from myapp import [module_under_test]

class Test[ModuleName]:
    """[Brief description of what this test suite covers]"""
    
    @pytest.fixture
    def [fixture_name](self):
        """[Fixture description]"""
        # Setup code
        yield [resource]
        # Teardown code
    
    def test_[scenario]_[expected_behavior](self, [fixtures]):
        """[Test description following Given/When/Then]"""
        # Arrange
        # Act
        # Assert
```

**Concrete example:**

```python
import pytest
from unittest.mock import Mock
from myapp.auth import authenticate_user

class TestAuthentication:
    """Test suite for user authentication functionality"""
    
    @pytest.fixture
    def mock_db(self):
        """Mock database connection for testing"""
        db = Mock()
        yield db
        db.close()
    
    def test_valid_credentials_returns_user_object(self, mock_db):
        """
        Given valid username and password
        When authenticate_user is called
        Then user object is returned with correct attributes
        """
        # Arrange
        mock_db.get_user.return_value = {"id": 1, "username": "alice"}
        
        # Act
        result = authenticate_user("alice", "correct_password", mock_db)
        
        # Assert
        assert result["id"] == 1
        assert result["username"] == "alice"
        mock_db.get_user.assert_called_once_with("alice")
```

**Generate 5-10 tests covering:**
- Happy path
- Invalid inputs
- Edge cases
- Error conditions
```

**Key elements:**
- Template shows structure
- Example shows implementation
- List what to cover
- Combine both for clarity

---

## Best Practices

### ✅ DO:
- Match strictness to task requirements
- Provide examples for ambiguous formats
- Specify what makes "good" output
- Allow flexibility where appropriate
- Show complete examples (not fragments)
- Use validation checklists for critical outputs

### ❌ DON'T:
- Over-specify creative tasks
- Use vague quality criteria ("make it good")
- Provide only template OR only examples (use both)
- Assume agent knows unstated conventions
- Create templates with unclear placeholders
- Mix multiple patterns without clear separation

---

## Pattern Selection Guide

| Pattern | Best For | Rigidity | Code Example | Docs Example | Data Example |
|---------|----------|----------|--------------|--------------|--------------|
| **Strict Template** | Compliance, APIs, formats | High | API responses | Legal docs | Schema definitions |
| **Flexible Guidance** | Analysis, reviews | Medium | Code reviews | Content reviews | Data quality reports |
| **Examples-Based** | Style/tone matching | Medium | Commit messages | Meeting notes | Data descriptions |
| **Validation Checklist** | Quality assurance | High | Test generation | Doc completeness | Schema validation |
| **Quality Tiers** | Variable depth needs | Low-High | API docs | Documentation | Data catalogs |
| **Hybrid** | Complex structured outputs | High | Test suites | API references | Pipeline configs |

Choose based on how critical exact format is vs. allowing agent judgment.

---

## Domain-Specific Output Examples

### Documentation Repository Outputs

#### Pattern 1: Strict Template for Meeting Notes
```markdown
## Meeting Notes Template

**ALWAYS use this exact structure:**

# [Meeting Type]: [Topic]
**Date:** YYYY-MM-DD
**Attendees:** @name1, @name2, @name3
**Facilitator:** @name

## Agenda
- [ ] Topic 1
- [ ] Topic 2

## Decisions Made
1. **Decision:** [What was decided]
   - **Rationale:** [Why]
   - **Owner:** @name
   - **Deadline:** YYYY-MM-DD

## Action Items
- [ ] **Task:** [Description]
  - **Owner:** @name
  - **Due:** YYYY-MM-DD
  - **Status:** Not Started

## Parking Lot
- [Topic deferred for later]

## Next Steps
- Next meeting: [Date/Time]
- Follow-up items: [List]
```

#### Pattern 2: Flexible Guidance for Documentation Reviews
```markdown
## Documentation Review Format

Use this structure as a guide, but adapt based on the content:

**For each issue found:**
1. **Location:** `file.md:line` or section heading
2. **Severity:** [Critical | Important | Suggestion]
3. **Issue:** What's wrong
4. **Suggestion:** How to fix

**Adjust as needed:**
- For typos: Just location + correction
- For structural issues: Add "Impact" section
- For outdated content: Add "Last updated" date

**Example:**
```
**Location:** getting-started.md:line 45
**Severity:** Critical
**Issue:** Installation command references deprecated package
**Suggestion:** Update to: `pip install new-package>=2.0`
```
```

#### Pattern 3: Examples-Based for Writing Style
```markdown
## Writing Style Examples

Follow these examples for tone and structure:

**Example 1: Feature Documentation**
Input: "Document the new export feature"

Output:
```markdown
# Exporting Data

You can now export your data in multiple formats.

**To export data:**
1. Navigate to the Data page
2. Click "Export" in the top-right corner
3. Select your preferred format (CSV, JSON, or Excel)
4. Click "Download"

**Supported formats:**
- CSV: Best for spreadsheets
- JSON: Best for programmatic access
- Excel: Best for detailed analysis

**Tip:** Large exports may take a few minutes to process.
```

**Example 2: API Reference**
Input: "Document the createUser endpoint"

Output:
```markdown
## POST /api/users

Creates a new user account.

**Request:**
```json
{
  "email": "user@example.com",
  "name": "Jane Doe",
  "role": "member"
}
```

**Response:** 201 Created
```json
{
  "id": "usr_123",
  "email": "user@example.com",
  "created_at": "2024-01-15T10:30:00Z"
}
```

**Errors:**
- 400: Invalid email format
- 409: Email already exists
```

Pattern: Concise, action-oriented, includes examples
```

### Data Repository Outputs

#### Pattern 1: Strict Template for Schema Documentation
```markdown
## Schema Documentation Template

**MUST follow this exact format:**

# Table: [table_name]

## Overview
[1-sentence description of table purpose]

## Schema

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, NOT NULL | Unique identifier |
| created_at | TIMESTAMP | NOT NULL, DEFAULT NOW() | Creation timestamp |
| updated_at | TIMESTAMP | NOT NULL | Last update timestamp |

## Relationships
- [table_name].[column] → [referenced_table].[referenced_column] (one-to-many)

## Indexes
- PRIMARY: id
- INDEX: created_at
- UNIQUE: email

## Example Query
```sql
SELECT * FROM [table_name]
WHERE created_at > NOW() - INTERVAL '7 days'
ORDER BY created_at DESC;
```

**Do not deviate from this structure.**
```

#### Pattern 4: Validation Checklist for Data Quality
```markdown
## Data Quality Validation

Your output must pass this checklist:

### Schema Compliance
- [ ] All required fields present
- [ ] Data types match schema
- [ ] Constraints satisfied (NOT NULL, UNIQUE)

### Data Quality
- [ ] No duplicate records (check primary key)
- [ ] All foreign keys reference existing records
- [ ] Numeric values within expected ranges
- [ ] Dates are valid and in correct format (ISO 8601)
- [ ] Strings don't exceed max length

### Business Rules
- [ ] Email addresses are valid
- [ ] Phone numbers match pattern
- [ ] Status values are from allowed enum

### Completeness
- [ ] No unexpected NULL values
- [ ] All mandatory relationships exist
- [ ] Audit fields populated (created_at, updated_at)

**If any check fails, report the specific issues before proceeding.**
```

---

## Cross-Domain Pattern: Quality Tiers

Works for code docs, documentation, and data - just adapt the criteria:

### Code Documentation (3 tiers)
- **Level 1:** Function signature + 1-line description
- **Level 2:** + Parameters, return values, examples
- **Level 3:** + Use cases, edge cases, performance notes

### User Documentation (3 tiers)
- **Level 1:** Quick reference (steps only)
- **Level 2:** + Context, examples, tips
- **Level 3:** + Tutorials, troubleshooting, FAQs

### Data Documentation (3 tiers)
- **Level 1:** Schema + column descriptions
- **Level 2:** + Relationships, indexes, examples
- **Level 3:** + Business rules, data lineage, quality metrics

**Pattern is universal - just customize the tier definitions!**
