#!/usr/bin/env python3

import re
import sys
import os
import argparse
from pathlib import Path

def extract_system_name_from_error(error_line):
    """Extract system name from error line"""
    # Pattern to extract system name from error lines like:
    # "2026-05-08T14:26:36.7626796Z 2026-05-08 14:26:36 Error executing openstack:debian-12-64:tests/upgrade/basic (may081403-684169) :"
    # or "Error executing openstack:debian-12-64:tests/upgrade/basic (may081403-684169) :"
    pattern = r'.* (?:Error executing|Error preparing|Error restoring) ([a-zA-Z0-9:-]+):'
    match = re.search(pattern, error_line)
    if match:
        return match.group(1)
    return None

def extract_backend_system_and_path(error_line):
    """Extract backend, system, and test path from error line"""
    # Pattern 1: "Error executing <backend>:<system>:<test-path> (<id>) :"
    # Pattern 2: "2024-05-01 23:13:09 Error executing <backend>:<system>:<test-path> (<id>) :"
    # This handles both formats with and without timestamps
    pattern = r'.* (?:Error executing|Error preparing|Error restoring) ([a-zA-Z0-9]+):([a-zA-Z0-9.-]+):([^ (]+) \([^)]+\)'
    match = re.search(pattern, error_line)
    if match:
        backend = match.group(1)
        system = match.group(2)
        test_path = match.group(3).strip()
        return backend, system, test_path
    # Try alternative pattern for format without ID
    pattern2 = r'.* (?:Error executing|Error preparing|Error restoring) ([a-zA-Z0-9]+):([a-zA-Z0-9.-]+):([^ (]+)'
    match2 = re.search(pattern2, error_line)
    if match2:
        backend = match2.group(1)
        system = match2.group(2)
        test_path = match2.group(3).strip()
        return backend, system, test_path
    return None, None, None

def generate_error_filename(backend, system, test_path):
    """Generate error filename from backend, system, and test path"""
    # Clean the test path by replacing problematic characters and removing IDs
    clean_test_path = test_path.replace('/', '_').replace(':', '_').replace('.', '_').replace('(', '_').replace(')', '_').replace(' ', '_')
    # Remove any trailing underscores and consecutive underscores
    clean_test_path = re.sub(r'_+', '_', clean_test_path).rstrip('_')
    # Limit length to avoid very long filenames
    clean_test_path = clean_test_path[:80]
    return f"{backend}_{system}_{clean_test_path}.log"

def extract_failed_tasks(log_file_path, output_dir="failed_tasks"):
    """Extract failed tasks from spread log file"""
    
    # Create output directory
    output_path = Path(output_dir)
    output_path.mkdir(exist_ok=True)
    
    # Pattern to match error lines (with or without timestamp prefix)
    error_patterns = [
        re.compile(r'.* Error executing'),
        re.compile(r'.* Error preparing'),
        re.compile(r'.* Error restoring')
    ]
    
    # Pattern to match debug output line (with or without timestamp prefix)
    debug_pattern = re.compile(r'^.* Debug output')
    
    current_system = None
    current_error = None
    error_lines = []
    failed_tasks = {}
    backend = None
    system = None
    test_path = None
    debug_filename = None
    
    try:
        with open(log_file_path, 'r', encoding='utf-8') as f:
            for line in f:
                line = line.rstrip('\n')
                
                # Check for error patterns
                is_error = any(pattern.match(line) for pattern in error_patterns)
                is_debug = debug_pattern.match(line)
                
                if is_error:
                    # Start new error block
                    current_error = line
                    current_system = extract_system_name_from_error(line)
                    backend, system, test_path = extract_backend_system_and_path(line)
                    error_lines = [line]
                    debug_filename = None  # Reset debug filename for new error
                elif current_system and not is_debug:
                    # Continue collecting error lines
                    error_lines.append(line)
                elif current_system and is_debug:
                    # End of error block - extract debug filename
                    debug_filename = extract_debug_filename(line)
                    if current_system not in failed_tasks:
                        failed_tasks[current_system] = []
                    failed_tasks[current_system].append({
                        'error': current_error,
                        'lines': error_lines.copy(),
                        'debug_filename': debug_filename,
                        'backend': backend,
                        'system': system,
                        'test_path': test_path
                    })
                    current_system = None
                    current_error = None
                    error_lines = []
                    debug_filename = None
                    backend = None
                    system = None
                    test_path = None
                
    except FileNotFoundError:
        print(f"Error: Log file '{log_file_path}' not found", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"Error reading log file: {e}", file=sys.stderr)
        sys.exit(1)
    
    return failed_tasks, output_dir

def save_failed_tasks(failed_tasks, output_dir="failed_tasks"):
    """Save failed task logs to separate files"""
    
    output_path = Path(output_dir)
    output_path.mkdir(exist_ok=True)
    used_filenames = {}
    
    files_saved = []
    for system, tasks in failed_tasks.items():
        for task in tasks:
            filename = None
            # Use extracted backend, system, and test path if available
            if task.get('backend') and task.get('system') and task.get('test_path'):
                filename = generate_error_filename(
                    task['backend'], 
                    task['system'], 
                    task['test_path']
                )
            else:
                # Fallback to debug filename if available
                if task.get('debug_filename'):
                    filename = generate_error_filename_from_debug(task['debug_filename'])
                    if not filename:
                        # If debug filename doesn't contain the structure, use simple fallback
                        debug_name = Path(task['debug_filename']).stem  # Remove .debug.log
                        debug_name = debug_name.replace('.debug', '')
                        filename = f"{system}_{debug_name}.log"
                else:
                    # Ultimate fallback - use timestamp or error summary
                    error_summary = task['error'][:40].replace(' ', '_').replace(':', '_')
                    safe_name = re.sub(r'[^\w\-]', '', error_summary)
                    filename = f"{system}_{safe_name}.log"
            
            # Handle duplicate filenames
            if filename in used_filenames:
                used_filenames[filename] += 1
                base_name = filename.rsplit('.', 1)[0]
                ext = filename.rsplit('.', 1)[1]
                filename = f"{base_name}_{used_filenames[filename]}.{ext}"
            else:
                used_filenames[filename] = 1
            
            filepath = output_path / filename
            
            with open(filepath, 'w', encoding='utf-8') as f:
                f.write(f"System: {system}\n")
                f.write(f"Error: {task['error']}\n")
                if task.get('debug_filename'):
                    f.write(f"Debug log: {task['debug_filename']}\n")
                f.write("=" * 50 + "\n")
                f.write("\n".join(task['lines']) + "\n")
            
            files_saved.append(str(filepath))
    
    return files_saved

def extract_debug_filename(debug_line):
    """Extract the debug filename from debug output line"""
    # Pattern: "Debug output for openstack:debian-12-64:tests/upgrade/basic (may081403-684169) : saved to file spread-logs/openstack_debian-12-64_tests_upgrade_basic.debug.log"
    pattern = r'saved to file (.*?\.debug\.log)'
    match = re.search(pattern, debug_line)
    if match:
        return match.group(1)
    return None

def generate_error_filename_from_debug(debug_filename):
    """Generate error filename from debug filename by extracting backend, system, and test path"""
    if debug_filename:
        # Extract filename without path
        just_filename = Path(debug_filename).name
        # Remove .debug.log extension and add .log
        if just_filename.endswith('.debug.log'):
            # Extract backend, system, and test path from filename like: openstack_debian-12-64_tests_upgrade_basic.debug.log
            parts = just_filename[:-len('.debug.log')].split('_')
            if len(parts) >= 3:
                backend = parts[0]
                system = parts[1]
                test_path = '_'.join(parts[2:])
                return generate_error_filename(backend, system, test_path)
    return None

def main():
    parser = argparse.ArgumentParser(
        description='Extract failed task logs from spread log files',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog='''
Examples:
  python3 parse_spread_log.py spread_log.txt
  python3 parse_spread_log.py spread_log.txt --output-dir ./failed_tasks
  python3 parse_spread_log.py spread_log.txt --output-dir /tmp/spread_failures
        ''')
    
    parser.add_argument('log_file', help='Path to the spread log file')
    parser.add_argument('--output-dir', default='failed_tasks',
                        help='Output directory for extracted logs (default: failed_tasks)')
    
    args = parser.parse_args()
    
    if not os.path.exists(args.log_file):
        print(f"Error: Log file '{args.log_file}' does not exist", file=sys.stderr)
        sys.exit(1)
    
    print(f"Parsing spread log: {args.log_file}")
    failed_tasks, output_dir = extract_failed_tasks(args.log_file, args.output_dir)
    
    if not failed_tasks:
        print("No failed tasks found")
        return
    
    print(f"Found {sum(len(tasks) for tasks in failed_tasks.values())} failed tasks across {len(failed_tasks)} systems")
    saved_files = save_failed_tasks(failed_tasks, output_dir)
    
    for filepath in saved_files:
        print(f"Saved: {filepath}")
    
    print("\nFailed task summary:")
    for system, tasks in failed_tasks.items():
        print(f"  {system}: {len(tasks)} failed task(s)")

if __name__ == "__main__":
    main()