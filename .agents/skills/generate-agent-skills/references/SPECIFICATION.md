# Agent Skill Technical Specification

## 1. Directory Naming Constraints (The Semantic Anchor)
The root directory identifies the skill and serves as the primary routing key.
* **Regex:** `^[a-z0-9][a-z0-9-]*[a-z0-9]$`
* **Constraint:** Lowercase, alphanumeric (`a-z`, `0-9`), and hyphens (`-`) only.
* **Length:** Max 64 characters.

## 2. File Structure & Roles
The filesystem uses a "Progressive Disclosure" hierarchy. Only `SKILL.md` is strictly mandatory.

| Path | Type | Requirement | Role |
| :--- | :--- | :--- | :--- |
| `SKILL.md` | File | **Mandatory** | The "Orchestrator". Contains metadata and routing logic. |
| `references/` | Directory | **Optional** | "Base Knowledge". Storage for static files (PDFs, JSON, CSV). |
| `scripts/` | Directory | **Optional** | "Executable Knowledge". Storage for code (Python, Bash). |
| `assets/` | Directory | **Optional** | Non-functional resources (images, raw templates). |

## 3. Metadata Specification (YAML Frontmatter)
The `SKILL.md` file must begin with a YAML block.

| Field | Required | Description |
| :--- | :--- | :--- |
| `name` | **Yes** | Must match the root directory name exactly. |
| `description` | **Yes** | Max 1024 chars. 3rd Person. Used for semantic routing. |
| `compatibility` | No | List of system dependencies (e.g., `python3`, `ffmpeg`). |
| `allowed-tools` | No | Security allowlist (e.g., `ls`, `grep`). |

## 4. The Progressive Disclosure Pattern
Structure your skill based on complexity.

### Type A: Pure Prompt Skill (Simple)
* **Use Case:** Summarization, simple tasks.
* **Structure:**
    ```text
    my-simple-skill/
    └── SKILL.md
    ```

### Type B: Tool-Backed Skill (Complex)
* **Use Case:** Data processing, API interaction, file manipulation.
* **Structure:**
    ```text
    my-complex-skill/
    ├── SKILL.md
    ├── references/   # Schema definitions
    └── scripts/      # Python logic
    ```

## 5. Deeper drive (if needed)

For more information, guide the user to: https://agentskills.io/specification