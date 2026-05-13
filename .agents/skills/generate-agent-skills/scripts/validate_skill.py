import os
import argparse
import re
import sys

def validate_skill(path):
    skill_dir = os.path.basename(os.path.normpath(path))
    skill_md_path = os.path.join(path, 'SKILL.md')
    errors = []
    warnings = []

    print(f"Validating skill: {skill_dir}...")

    # 1. CRITICAL: Identity & Naming (Strict)
    if not re.match(r'^[a-z0-9][a-z0-9-]*[a-z0-9]$', skill_dir):
        errors.append("Directory name violates regex (lowercase/hyphens only).")

    # 2. CRITICAL: The Entry Point (Strict)
    if not os.path.exists(skill_md_path):
        errors.append("Missing SKILL.md (The mandatory entry point).")
    
    # 3. CRITICAL: Metadata Consistency (Strict)
    if os.path.exists(skill_md_path):
        with open(skill_md_path, 'r') as f:
            content = f.read()
        
        name_match = re.search(r'^name:\s*(.+)$', content, re.MULTILINE)
        if not name_match:
            errors.append("YAML frontmatter missing 'name' field.")
        elif name_match.group(1).strip() != skill_dir:
            errors.append(f"YAML name '{name_match.group(1).strip()}' mismatches directory '{skill_dir}'.")

        if not re.search(r'^description:\s*(.+)$', content, re.MULTILINE):
            errors.append("YAML frontmatter missing 'description' field.")

    # 4. ADVISORY: Progressive Disclosure (Flexible)
    # We check if they exist, but we do not fail if they don't.
    has_refs = os.path.isdir(os.path.join(path, 'references'))
    has_scripts = os.path.isdir(os.path.join(path, 'scripts'))

    if not has_refs:
        warnings.append("No 'references/' directory found. (Acceptable for simple skills).")
    
    if not has_scripts:
        warnings.append("No 'scripts/' directory found. (Acceptable for pure-prompt skills).")

    # Final Report
    if errors:
        print("\n[FAIL] Critical Violations:")
        for e in errors: print(f"  ❌ {e}")
        sys.exit(1)
    else:
        print("\n[PASS] Skill is valid.")
        if warnings:
            print("[INFO] Architectural Notes:")
            for w in warnings: print(f"  ℹ️  {w}")

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description='Validate an Agent Skill.')
    parser.add_argument('--path', required=True, help='Skill root path')
    args = parser.parse_args()
    validate_skill(args.path)