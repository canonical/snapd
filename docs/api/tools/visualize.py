# SPDX-FileCopyrightText: 2025 Canonical Ltd
# SPDX-License-Identifier: GPL-3.0-only

import yaml
import sys
import os
from collections import defaultdict

def find_refs(data):
    """Recursively finds all '$ref' values in the data."""
    refs = set()
    if isinstance(data, dict):
        for k, v in data.items():
            if k == '$ref' and isinstance(v, str):
                refs.add(v)
            else:
                refs.update(find_refs(v))
    elif isinstance(data, list):
        for item in data:
            refs.update(find_refs(item))
    return refs

def generate_dot_file(output_filename, source_nodes, endpoint_label, target_groups, all_edges, dark_mode=False):
    """
    Generates a DOT graph file combining multiple target types with a dark mode option.
    """
    # --- Define color palettes ---
    if dark_mode:
        palette = {
            "bgcolor": "#202020",
            "fontcolor": "#d0d0d0",
            "cluster_color": "#CDCDCD",
            "default_edge": "#CDCDCD",
            "endpoints": "#606060",
            "schemas": "#F99B11",
            "responses": "#24598F",
            "security": "#C7162B",
            "schemas_edge": "#F99B11",
            "responses_edge": "#24598F",
            "security_edge": "#C7162B"
        }
    else:
        palette = {
            "bgcolor": "#f8f8f8",
            "fontcolor": "#000000",
            "cluster_color": "#303030",
            "default_edge": "#303030",
            "endpoints": "#606060",
            "schemas": "#F99B11",
            "responses": "#24598F",
            "security": "#C7162B",
            "schemas_edge": "#F99B11",
            "responses_edge": "#24598F",
            "security_edge": "#C7162B"
        }

    with open(output_filename, 'w') as f:
        f.write('digraph OpenAPI_Dependencies {\n')
        f.write(f'  bgcolor="{palette["bgcolor"]}";\n')
        f.write(f'  fontcolor="{palette["fontcolor"]}";\n')
        f.write('  rankdir="TB";\n')
        f.write('  splines="curved";\n')
        f.write('  compound=true;\n')
        f.write('  concentrate=true;\n')
        f.write('  graph [nodesep=0.5, ranksep=2.5];\n')
        f.write(f'  node [shape=box, style="rounded,filled", fontcolor="{palette["fontcolor"]}"];\n')

        # Endpoints Subgraph
        f.write('  subgraph cluster_endpoints {\n')
        f.write(f'    label = "{endpoint_label}";\n')
        f.write(f'    fontcolor = "{palette["cluster_color"]}";\n')
        f.write(f'    color = "{palette["cluster_color"]}";\n')
        f.write('    margin = 10;\n')
        f.write(f'    node [fillcolor="{palette["endpoints"]}"];\n')
        for node in sorted(list(source_nodes)):
            f.write(f'    "{node}";\n')
        f.write('  }\n\n')

        # Target Subgraphs
        for target_label, target_nodes in target_groups.items():
            if target_nodes:
                f.write(f'  subgraph cluster_{target_label.lower()} {{\n')
                f.write(f'    label = "{target_label}";\n')
                f.write(f'    fontcolor = "{palette["cluster_color"]}";\n')
                f.write(f'    color = "{palette["cluster_color"]}";\n')
                f.write('    margin = 10;\n')
                f.write(f'    node [fillcolor="{palette.get(target_label.lower(), "#ffffff")}"];\n')
                for node in sorted(list(target_nodes)):
                    f.write(f'    "{node}";\n')
                f.write('  }\n\n')

        # Logic to color edges based on their target type
        # Create a reverse mapping from a node name to its type (e.g., "User" -> "schemas")
        node_to_type = {}
        for target_label, target_nodes in target_groups.items():
            for node in target_nodes:
                node_to_type[node] = target_label.lower()

        f.write('  // Edges (Dependencies)\n')
        for source, target in sorted(list(all_edges)):
            target_type = node_to_type.get(target, "")
            edge_color = palette.get(f"{target_type}_edge", palette["default_edge"])
            f.write(f'  "{source}" -> "{target}" [color="{edge_color}"];\n')
        
        f.write('}\n')
    print(f"Successfully generated DOT file at '{output_filename}'")

def main(dark_mode=False, max_edges=None):
    """Main function to parse the spec and generate graphs."""
    # The script is now designed to run from the project root.
    # It expects the bundled OpenAPI spec to be in the same directory.
    spec_file = 'openapi-bundled.yaml'
    project_root = os.getcwd()
    
    # Determine output directory based on dark mode
    theme = 'dark' if dark_mode else 'light'
    output_dir = os.path.join(project_root, 'visuals', theme)
    
    os.makedirs(output_dir, exist_ok=True)
    print(f"Output directory '{output_dir}' is ready.")

    try:
        with open(spec_file, 'r') as f:
            spec = yaml.safe_load(f)
    except FileNotFoundError:
        print(f"Error: The file '{spec_file}' was not found.")
        print("Please make sure to run 'make bundle' to generate the bundled OpenAPI specification.")
        sys.exit(1)
    except yaml.YAMLError as e:
        print(f"Error parsing YAML file: {e}")
        sys.exit(1)

    valid_http_methods = {'get', 'put', 'post', 'delete', 'options', 'head', 'patch', 'trace'}

    paths_by_tag = defaultdict(dict)
    for path, path_item in spec.get('paths', {}).items():
        for method in set(path_item.keys()) & valid_http_methods:
            operation = path_item[method]
            if not isinstance(operation, dict):
                continue

            tags = operation.get('tags', ['untagged'])
            for tag in tags:
                if path not in paths_by_tag[tag]:
                    paths_by_tag[tag][path] = {}
                paths_by_tag[tag][path][method] = operation

    for tag, paths in paths_by_tag.items():
        print(f"\n--- Processing tag: {tag} ---")
        
        source_nodes = set()
        edges_to_schemas = set()
        edges_to_responses = set()
        edges_to_security = set()

        for path, path_item in paths.items():
            for method, operation in path_item.items():
                path_node_name = f"{method.upper()} {path}"
                source_nodes.add(path_node_name)
            
                if 'security' in operation and operation['security'] is not None:
                    for security_req in operation['security']:
                        for scheme_name in security_req.keys():
                            edges_to_security.add((path_node_name, scheme_name))

                all_refs = find_refs(operation)
                for ref in all_refs:
                    target_name = ref.split('/')[-1]
                    if '#/components/schemas/' in ref:
                        edges_to_schemas.add((path_node_name, target_name))
                    elif '#/components/responses/' in ref:
                        edges_to_responses.add((path_node_name, target_name))

        endpoint_label = f"Endpoints - {tag}"

        all_edges = edges_to_schemas.union(edges_to_responses).union(edges_to_security)
        
        if max_edges is not None and len(all_edges) > max_edges:
            print(f"Skipping DOT file generation for tag '{tag}' because it has {len(all_edges)} edges, which exceeds the max of {max_edges}.")
            continue

        if all_edges:
            schema_nodes = {edge[1] for edge in edges_to_schemas}
            response_nodes = {edge[1] for edge in edges_to_responses}
            security_nodes = {edge[1] for edge in edges_to_security}
            
            target_groups = {
                "Schemas": schema_nodes,
                "Responses": response_nodes,
                "Security": security_nodes
            }
            
            output_path = os.path.join(output_dir, f'{tag.replace(" ", "_")}_dependencies.dot')
            generate_dot_file(output_path, source_nodes, endpoint_label, target_groups, all_edges, dark_mode)

if __name__ == '__main__':
    import argparse
    parser = argparse.ArgumentParser(description='Generate DOT dependency graphs from an OpenAPI specification.')
    parser.add_argument('--dark', action='store_true', help='Enable dark mode for the generated graphs.')
    parser.add_argument('--max-edges', type=int, help='Skip graph generation if the number of edges exceeds this limit.')
    args = parser.parse_args()

    main(dark_mode=args.dark, max_edges=args.max_edges)
