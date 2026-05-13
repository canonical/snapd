import os
import argparse
import re
import sys
from pathlib import Path
import subprocess

# Template content for generated files
SKILL_MD_TEMPLATE = """---
name: {skill_name}
description: [TODO: Complete and informative explanation of what the skill does and when to use it. Include WHEN to use this skill - specific scenarios, file types, or tasks that trigger it.]
---

# {skill_title}

## Overview

[TODO: 1-2 sentences explaining what this skill enables]

## Structuring This Skill

[TODO: Choose the structure that best fits this skill's purpose. Common patterns:

**1. Workflow-Based** (best for sequential processes)
- Works well when there are clear step-by-step procedures
- Example: Document processing with "Analyze" → "Extract" → "Validate" → "Report"
- Structure: ## Overview → ## Workflow → ## Step 1 → ## Step 2...
- See `references/workflows.md` for workflow patterns

**2. Task-Based** (best for tool collections)
- Works well when the skill offers different operations/capabilities
- Example: PDF skill with "Merge PDFs" → "Split PDFs" → "Extract Text"
- Structure: ## Overview → ## Quick Start → ## Task Category 1 → ## Task Category 2...

**3. Reference/Guidelines** (best for standards or specifications)
- Works well for brand guidelines, coding standards, or requirements
- Example: Brand styling with "Brand Guidelines" → "Colors" → "Typography"
- Structure: ## Overview → ## Guidelines → ## Specifications → ## Usage...

**4. Capabilities-Based** (best for integrated systems)
- Works well when the skill provides multiple interrelated features
- Example: Data analysis with numbered capability list
- Structure: ## Overview → ## Core Capabilities → ### 1. Feature → ### 2. Feature...

Patterns can be mixed and matched. Most skills combine patterns.

Delete this entire "Structuring This Skill" section when done - it's just guidance.]

## Resources

This skill includes example resource directories. Customize or delete as needed:

### scripts/
Executable code (Python/Bash/etc.) for deterministic operations.
- See `scripts/example.py` for a starter template
- **When to use:** Math, file parsing, API calls, automation

### references/
Documentation loaded into context to inform the agent's thinking.
- See `references/example_reference.md` for structure suggestions
- **When to use:** API docs, schemas, detailed guides, domain knowledge
- **Tip:** For large files (>10k words), mention grep patterns in this SKILL.md

### assets/
Files used in output (templates, images, fonts) - NOT loaded into context.
- See `assets/README.md` for explanation
- **When to use:** Boilerplate code, templates, images, fonts

**Delete any unneeded directories.** Not every skill requires all three.

## [TODO: Replace with your main section]

[TODO: Add your skill's core content here. Consider:
- Code samples for technical skills (see `references/output-patterns.md` for patterns)
- Decision trees for complex workflows (see `references/workflows.md`)
- Concrete examples with realistic user requests
- References to scripts/templates/references as needed]

## Next Steps

After creating this skill:
1. Complete all TODO items in this file
2. Update the description to be specific and keyword-rich
3. Customize or delete example files in scripts/, references/, and assets/
4. Run validation: `python3 scripts/validate_skill.py --path .github/skills/{skill_name}`
5. Test the skill with real use cases
6. Iterate based on feedback
"""

EXAMPLE_SCRIPT_TEMPLATE = '''#!/usr/bin/env python3
"""
Example helper script for {skill_name}

This is a placeholder demonstrating how to structure executable scripts.
Replace with actual implementation or delete if not needed.

Example real scripts from other skills:
- Data processing: Parse CSV, transform JSON, aggregate results
- File operations: Convert formats, merge files, validate structure
- API interaction: Fetch data, authenticate, handle rate limits

Usage:
    python3 scripts/example.py --arg value
"""

import argparse
import sys

def main():
    parser = argparse.ArgumentParser(description="Example script for {skill_name}")
    parser.add_argument("--arg", help="Example argument", required=False)
    args = parser.parse_args()
    
    print(f"Example script executed for {skill_name}")
    print(f"Argument: {{args.arg}}")
    
    # TODO: Add actual script logic here
    # This could be:
    # - Data processing (pandas, numpy)
    # - File conversion (pdf, docx, images)
    # - API calls (requests, authentication)
    # - Validation (schema checking, linting)
    
    return 0

if __name__ == "__main__":
    sys.exit(main())
'''

EXAMPLE_REFERENCE_TEMPLATE = r"""# Reference Documentation for {skill_title}

This is a placeholder for detailed reference documentation.
Replace with actual reference content or delete if not needed.

## When Reference Docs Are Useful

Reference docs are ideal for:
- **Comprehensive guides** - Multi-page documentation that would bloat SKILL.md
- **API documentation** - Endpoint specifications, parameters, examples
- **Schemas** - Database schemas, data models, type definitions
- **Domain knowledge** - Company policies, industry standards, legal templates
- **Detailed workflows** - Step-by-step procedures with screenshots/diagrams

## Structure Suggestions

Choose a structure that fits your content:

### API Reference Example
```markdown
## Authentication
- Method: Bearer token
- Header: `Authorization: Bearer <token>`

## Endpoints

### GET /api/users
**Description:** Retrieve user list
**Parameters:**
- `page` (int, optional): Page number (default: 1)
- `limit` (int, optional): Results per page (default: 20)

**Response:**
\`\`\`json
{{
  "users": [...],
  "total": 150,
  "page": 1
}}
\`\`\`

**Error codes:**
- 401: Unauthorized
- 429: Rate limit exceeded
```

### Workflow Guide Example
```markdown
## Prerequisites
- Python 3.8+
- API credentials configured
- Required packages: `pip install -r requirements.txt`

## Step 1: Initialize Connection
1. Import the client: `from myapi import Client`
2. Create instance: `client = Client(api_key='...')`
3. Test connection: `client.ping()`

## Step 2: Fetch Data
[Detailed instructions...]

## Troubleshooting
**Problem:** Connection timeout
**Solution:** Check firewall settings...
```

### Schema Documentation Example
```markdown
## Database Schema

### Table: users
| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | INTEGER | PRIMARY KEY | Unique user ID |
| email | VARCHAR(255) | UNIQUE, NOT NULL | User email |
| created_at | TIMESTAMP | DEFAULT NOW() | Registration time |

### Relationships
- users.id → orders.user_id (one-to-many)
- users.id → profiles.user_id (one-to-one)
```

## Tips for Large Reference Files

If this file will be >100 lines:
- Include a **Table of Contents** at the top
- Use clear section headers (##, ###)
- Add grep hints in main SKILL.md for easier navigation

## Best Practices

✅ **DO:**
- Keep SKILL.md lean, move details here
- Use tables for structured data
- Include concrete examples
- Link back to SKILL.md where relevant

❌ **DON'T:**
- Duplicate content from SKILL.md
- Create reference docs for <50 lines of content (put in SKILL.md)
- Use vague descriptions (be specific)
- Forget to mention this file exists in SKILL.md
"""

ASSETS_README_TEMPLATE = """# Assets Directory

This directory contains files that are **used in output** - NOT loaded into context.

## What Goes Here?

### Templates
Files that get copied or modified to create final output:
- Document templates: `.docx`, `.pptx`, `.pdf`
- Code boilerplate: Starter projects, directory structures
- Configuration templates: `.yaml`, `.json`, `.toml`

### Visual Assets
Images, icons, and graphics used in generated content:
- Logos: `.png`, `.svg`
- Icons: `.ico`, `.svg`
- Diagrams: `.png`, `.jpg`, `.svg`

### Fonts
Typography files for document generation:
- Font files: `.ttf`, `.otf`, `.woff`, `.woff2`

### Data Files
Sample or seed data (not documentation):
- Sample datasets: `.csv`, `.json`, `.xml`
- Test fixtures: Example inputs for testing
- Seed data: Initial data for databases

## What Does NOT Go Here?

❌ **Documentation** → Use `references/` instead  
❌ **Executable code** → Use `scripts/` instead  
❌ **Instructions** → Put in `SKILL.md`

## Examples from Other Skills

### Brand Guidelines Skill
```
assets/
├── logo.png              # Company logo
├── slides_template.pptx  # PowerPoint template
└── brand_colors.json     # Color palette data
```

### Frontend Builder Skill
```
assets/
└── react-starter/        # Boilerplate React project
    ├── package.json
    ├── src/
    │   ├── App.jsx
    │   └── index.js
    └── public/
        └── index.html
```

### Document Generator Skill
```
assets/
├── contract_template.docx
├── invoice_template.xlsx
└── fonts/
    ├── roboto-regular.ttf
    └── roboto-bold.ttf
```

## How Agents Use Assets

Agents typically:
1. **Copy** the asset to a new location
2. **Modify** it based on user input (fill template fields, replace placeholders)
3. **Return** the modified file to the user

Assets are **never loaded into context** - they're treated as binary blobs that get manipulated.

## Tips

- Keep assets organized in subdirectories if many files
- Use descriptive names: `invoice_template.xlsx` not `template1.xlsx`
- Include a comment in SKILL.md about what assets exist
- Remove this README if you don't use assets (not every skill needs them)
"""

def title_case_skill_name(skill_name):
    """Convert hyphenated skill name to Title Case."""
    return ' '.join(word.capitalize() for word in skill_name.split('-'))

def find_skills_dir():
    """Find the .github/skills directory."""
    # Try to find git root first
    try:
        result = subprocess.run(
            ['git', 'rev-parse', '--show-toplevel'],
            capture_output=True,
            text=True,
            check=True
        )
        git_root = Path(result.stdout.strip())
        skills_dir = git_root / '.github' / 'skills'
        if skills_dir.exists():
            return skills_dir
    except (subprocess.CalledProcessError, FileNotFoundError):
        pass
    
    # Search upward from CWD
    current = Path.cwd()
    for parent in [current, *current.parents]:
        skills_dir = parent / '.github' / 'skills'
        if skills_dir.exists():
            return skills_dir
    
    # Fallback to CWD (backward compatibility)
    print("⚠️  Warning: Could not find .github/skills, creating in current directory.")
    return current

def scaffold_skill(skill_name, simple_mode=False):
    # 1. Validation
    if not re.match(r'^[a-z0-9][a-z0-9-]*[a-z0-9]$', skill_name):
        print(f"❌ Error: Name '{skill_name}' violates naming constraints.")
        print("   Must be lowercase alphanumeric with hyphens, e.g., 'my-skill-name'")
        sys.exit(1)

    # 2. Determine target directory
    skills_root = find_skills_dir()
    skill_path = skills_root / skill_name
    
    # 3. Check if directory already exists
    if skill_path.exists():
        print(f"❌ Error: Skill directory already exists: {skill_path}")
        sys.exit(1)
    
    try:
        skill_path.mkdir(parents=True, exist_ok=False)
        print(f"✅ Created skill directory: {skill_path}")
    except Exception as e:
        print(f"❌ Error creating directory: {e}")
        sys.exit(1)
    
    # 4. Create SKILL.md with rich template
    skill_title = title_case_skill_name(skill_name)
    skill_content = SKILL_MD_TEMPLATE.format(
        skill_name=skill_name,
        skill_title=skill_title
    )
    
    skill_md_path = skill_path / 'SKILL.md'
    try:
        skill_md_path.write_text(skill_content)
        print("✅ Created SKILL.md with structuring guidance")
    except Exception as e:
        print(f"❌ Error creating SKILL.md: {e}")
        sys.exit(1)
    
    # 5. Create resource directories with example files (unless simple mode)
    if not simple_mode:
        try:
            # Create scripts/ directory with example script
            scripts_dir = skill_path / 'scripts'
            scripts_dir.mkdir(exist_ok=True)
            example_script = scripts_dir / 'example.py'
            example_script.write_text(EXAMPLE_SCRIPT_TEMPLATE.format(skill_name=skill_name))
            example_script.chmod(0o755)
            print("✅ Created scripts/example.py")
            
            # Create references/ directory with example reference doc
            references_dir = skill_path / 'references'
            references_dir.mkdir(exist_ok=True)
            example_reference = references_dir / 'example_reference.md'
            example_reference.write_text(EXAMPLE_REFERENCE_TEMPLATE.format(skill_title=skill_title))
            print("✅ Created references/example_reference.md")
            
            # Create assets/ directory with README
            assets_dir = skill_path / 'assets'
            assets_dir.mkdir(exist_ok=True)
            assets_readme = assets_dir / 'README.md'
            assets_readme.write_text(ASSETS_README_TEMPLATE)
            print("✅ Created assets/README.md")
        except Exception as e:
            print(f"❌ Error creating resource directories: {e}")
            sys.exit(1)
    else:
        print("ℹ️  Simple mode: Skipped creating example resource directories")
    
    # 6. Print next steps
    print(f"\n🎉 Skill '{skill_name}' initialized successfully!")
    print(f"📁 Location: {skill_path}")
    print("\n📋 Next steps:")
    print("   1. Edit SKILL.md to complete the TODO items")
    print("   2. Update the description to be specific and keyword-rich")
    if not simple_mode:
        print("   3. Customize or delete the example files in scripts/, references/, and assets/")
        print("   4. Consult references/workflows.md and output-patterns.md for guidance")
        print(f"   5. Validate: python3 scripts/validate_skill.py --path {skill_path}")
    else:
        print(f"   3. Validate: python3 scripts/validate_skill.py --path {skill_path}")
    print("   6. Test with real use cases and iterate!")

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument('--name', required=True)
    parser.add_argument('--simple', action='store_true', help="Create a minimal skill without subdirectories")
    args = parser.parse_args()
    scaffold_skill(args.name, args.simple)