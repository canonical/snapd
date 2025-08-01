
import copy
import dash
from dash import Dash, html, Output, Input, dash_table, State, MATCH, ALL
import dash_bootstrap_components as dbc
import json
import os
import sys

# To ensure this can be run from any point in the filesystem,
# add parent folder to path to permit relative imports
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

import query_features as qf

suppress_callback_exceptions = True
app = Dash(__name__, external_stylesheets=[dbc.themes.BOOTSTRAP])

retriever = None
timestamps = None
timestamp_options = []
coverage_matrix = {}
feature_options = [{"label": item, "value": item} for item in qf.KNOWN_FEATURES]
cached_duplicates = {}
cached_all_features_diff = {}
cached_feat_explore = {}


@app.callback(
    Output("error-with-retriever", "displayed"),
    Output({"type": "toggle-button", "index": ALL}, "disabled"),
    Output({"type": "timestamp-dropdown", "index": ALL}, "options"),
    Output("empty-div", "children"),
    Input("set-data-button", "n_clicks"),
    State("input-path", "value"),
)
def set_retriever(_, path):
    if not path:
        raise dash.exceptions.PreventUpdate

    if not os.path.exists(path):
        # First return true to display the error message.
        # Next, there are 5 toggle buttons and 6 timestamp dropdowns. 
        # Make all of the toggle buttons disabled and clear out
        # the options for all timestamp dropdowns.
        # Finally, return an empty output to signal the 
        # dcc.loading that this callback has finished.
        return True, [True] * 5, [[], [], [], [], [], []], []
    global retriever
    if os.path.isfile(path):
        with open(path, "r", encoding="utf-8") as f:
            retriever = qf.MongoRetriever(f)
    elif os.path.isdir(path):
        retriever = qf.DirRetriever(path)
    global timestamps
    timestamps = retriever.get_sorted_timestamps_and_systems()
    global timestamp_options
    timestamp_options = [
        {"label": item["timestamp"], "value": item["timestamp"]} for item in timestamps
    ]
    # First, return false so the error message is not displayed
    # Enable all 5 of the toggle buttons.
    # Populate all 6 timestamp dropdowns with the values.
    # Finally, return an empty output to signal the dcc.loading
    # that this callback has finished.
    return False, [False] * 5, [timestamp_options] * 6, []


def get_columns_from_list_of_dicts(features):
    i, _ = max(enumerate(features), key=lambda x: len(x[1]))
    return [{"name": key, "id": key} for key in features[i].keys()]


def make_dict_table_friendly(features):
    """
    For dictionary values that are themselves dictionaries,
    changes them to their string representation
    """
    processed = []
    for feature in features:
        feat_dict = {}
        for k, v in feature.items():
            feat_dict[k] = json.dumps(v) if isinstance(v, list) else v
        processed.append(feat_dict)
    return processed


@app.callback(
    Output({"type": "collapse", "index": MATCH}, "is_open"),
    Input({"type": "toggle-button", "index": MATCH}, "n_clicks"),
    State({"type": "collapse", "index": MATCH}, "is_open"),
)
def toggle_collapse(n_clicks, is_open):
    if n_clicks:
        return not is_open
    return is_open


@app.callback(
    Output({"type": "systems-dropdown", "index": MATCH}, "options"),
    Input({"type": "timestamp-dropdown", "index": MATCH}, "value"),
)
def update_systems_dropdown(selected_timestamp):
    if not selected_timestamp:
        return []
    for item in timestamps:
        if item["timestamp"] == selected_timestamp:
            return [{"label": sys, "value": sys} for sys in item["systems"]]
    return []


@app.callback(
    Output("all-features-diff-table", "columns"),
    Output("all-features-diff-table", "data"),
    Input({"type": "timestamp-dropdown", "index": 1}, "value"),
)
def update_totals_table(selected_timestamp):
    if not selected_timestamp:
        return [], []

    diff_data = []
    for timestamp in timestamps:
        if timestamp["timestamp"] != selected_timestamp:
            continue
        for system in timestamp["systems"]:
            if (
                selected_timestamp in cached_all_features_diff
                and system in cached_all_features_diff[selected_timestamp]
            ):
                diff = cached_all_features_diff[selected_timestamp][system]
            else:
                diff = qf.diff_all_features(
                    retriever, selected_timestamp, system, False
                )
                if selected_timestamp not in cached_all_features_diff:
                    cached_all_features_diff[selected_timestamp] = {}
                cached_all_features_diff[selected_timestamp][system] = diff

            d = {key: len(value) for key, value in diff.items()}
            d["system"] = system
            diff_data.append(d)

    columns = [{"name": "System", "id": "system"}] + [
        {"name": key, "id": key} for key in qf.KNOWN_FEATURES
    ]

    return columns, diff_data


@app.callback(
    Output("all-features-diff-cell-data-container", "children"),
    Input("all-features-diff-table", "active_cell"),
    State("all-features-diff-table", "derived_viewport_data"),
    State({"type": "timestamp-dropdown", "index": 1}, "value"),
)
def display_column_details(active_cell, table_data, timestamp):
    if active_cell is None:
        return "Click a column cell to see details."

    row_idx = active_cell["row"]
    col_name = active_cell["column_id"]
    test = table_data[row_idx]

    features = cached_all_features_diff[timestamp][test["system"]][col_name]
    processed = []
    for feature in features:
        feat_dict = {}
        for k, v in feature.items():
            feat_dict[k] = json.dumps(v) if isinstance(v, list) else v
        processed.append(feat_dict)

    table = dash_table.DataTable(
        data=processed,
        columns=get_columns_from_list_of_dicts(features),
        filter_action="native",
        sort_action="native",
        style_cell={
            "textAlign": "center",
            "maxWidth": "100%",
            "whiteSpace": "normal",
        },
        style_table={"overflowX": "auto", "maxWidth": "100%", "margin": "auto"},
    )

    return [html.H4(f"{test['system']} -- {col_name}"), table]


@app.callback(
    Output("coverage-diff-container", "children"),
    Input({"type": "timestamp-dropdown", "index": 2}, "value"),
    Input({"type": "systems-dropdown", "index": 2}, "value"),
    Input({"type": "timestamp-dropdown", "index": 3}, "value"),
    Input({"type": "systems-dropdown", "index": 3}, "value"),
    Input("remove-failed-switch", "on"),
    Input("only-same-switch", "on"),
)
def update_totals_table(ts_2, sys_2, ts_3, sys_3, remove_failed_value, only_same_value):
    if not ts_2 or not sys_2 or not ts_3 or not sys_3:
        return []

    diff = qf.diff(
        retriever, ts_2, sys_2, ts_3, sys_3, remove_failed_value, only_same_value
    )

    tables = []
    i = 0
    for feature_name, features in reversed(diff.items()):
        processed = []
        for feature in features:
            feat_dict = {}
            for k, v in feature.items():
                feat_dict[k] = json.dumps(v) if isinstance(v, list) else v
            processed.append(feat_dict)
        table = dash_table.DataTable(
            id={"type": "coverage-diff-table", "index":i},
            data=processed,
            columns=get_columns_from_list_of_dicts(features),
            filter_action="native",
            sort_action="native",
            style_cell={
                "textAlign": "center",
                "maxWidth": "100%",
                "whiteSpace": "normal",
            },
            style_table={"overflowX": "auto", "maxWidth": "100%", "margin": "auto"},
        )
        tables.append(
            html.Div(
                [html.H4(feature_name), table],
                style={"maxWidth": "100%", "margin": "auto"},
            )
        )
        i=i+1

    return tables

@app.callback(
    Output("coverage-diff-modal-body", "children"),
    Output("coverage-diff-modal", "is_open"),
    Input({"type": "coverage-diff-table", "index": ALL}, "active_cell"),
    State({"type": "coverage-diff-table", "index": ALL}, "derived_viewport_data"),
    State({"type": "timestamp-dropdown", "index": 2}, "value"),
    State({"type": "systems-dropdown", "index": 2}, "value"),
)
def populate_tests_in_coverage_diff_cmds(active_cell, table_data, timestamp, system):
    triggered = dash.callback_context.triggered_id
    if not active_cell or not table_data or not triggered or not 'index' in triggered or not active_cell[triggered["index"]] or not table_data[triggered["index"]]:
        raise dash.exceptions.PreventUpdate

    row_idx = active_cell[triggered["index"]]["row"]
    feature = copy.deepcopy(table_data[triggered["index"]][row_idx])
    for k, v in feature.items():
        try:
            feature[k] = json.loads(v)
        except:
            pass

    results = qf.find_feat(retriever, timestamp, feature, remove_failed=False, system=system, force_exact=True)
    table = dash_table.DataTable(
        data=get_task_list_from_dict(results),
        columns=[{"name":"suite","id":"suite"}, {"name":"test","id":"test"}, {"name":"variant","id":"variant"}],
        filter_action="native",
        sort_action="native",
        style_cell={
            "textAlign": "center",
            "maxWidth": "100%",
            "whiteSpace": "normal",
        },
        style_table={"overflowX": "auto", "maxWidth": "100%", "minWidth": "600px", "margin": "auto"},
    )
    return html.Div(
        [html.H4(f"{system} --- {feature}", style={"textAlign": "center"}), table],
        style={"maxWidth": "100%", "margin": "auto"},
    ), True


@app.callback(
    Output({"type": "timestamp-dropdown", "index": 2}, "value"),
    Output({"type": "systems-dropdown", "index": 2}, "value"),
    Output({"type": "timestamp-dropdown", "index": 3}, "value"),
    Output({"type": "systems-dropdown", "index": 3}, "value"),
    Input("switch-button", "n_clicks"),
    State({"type": "timestamp-dropdown", "index": 2}, "value"),
    State({"type": "systems-dropdown", "index": 2}, "value"),
    State({"type": "timestamp-dropdown", "index": 3}, "value"),
    State({"type": "systems-dropdown", "index": 3}, "value"),
)
def switch_dropdown_values(n_clicks, ts2, sys2, ts3, sys3):
    if n_clicks is None or n_clicks == 0:
        raise dash.exceptions.PreventUpdate

    return ts3, sys3, ts2, sys2


@app.callback(
    Output("suite-dropdown-filter", "options"),
    Output("task-dropdown-filter", "options"),
    Output("variant-dropdown-filter", "options"),
    Input({"type": "timestamp-dropdown", "index": 4}, "value"),
)
def update_systems_dropdown(selected_timestamp):
    if not selected_timestamp:
        raise dash.exceptions.PreventUpdate
    task_list = qf.task_list(retriever, selected_timestamp)
    suites = set([task.suite for task in task_list])
    tasks = set([task.task_name for task in task_list])
    variants = set([task.variant for task in task_list])
    return list(suites), list(tasks), list(variants)


@app.callback(
    Output("coverage-matrix-table", "columns"),
    Output("coverage-matrix-table", "data"),
    Input({"type": "timestamp-dropdown", "index": 4}, "value"),
    Input("coverage-remove-failed-switch", "on"),
    Input("suite-dropdown-filter", "value"),
    Input("task-dropdown-filter", "value"),
    Input("variant-dropdown-filter", "value"),
)
def create_coverage_matrix(timestamp, remove_failed, suite, task, variant):
    if not dash.callback_context.triggered:
        raise dash.exceptions.PreventUpdate

    systems = None
    for ts in timestamps:
        if ts["timestamp"] == timestamp:
            systems = ts["systems"]
    if not systems:
        return [], []

    columns = [{"name": "System", "id": "system"}] + [
        {"name": key, "id": key} for key in qf.KNOWN_FEATURES
    ]
    coverage_matrix[timestamp] = [
        {"system": system, **{key: 0 for key in qf.KNOWN_FEATURES}}
        for system in systems
    ]
    matrix = [
        {"system": system, **{key: 0 for key in qf.KNOWN_FEATURES}}
        for system in systems
    ]
    for i, system in enumerate(systems):
        feats = qf.feat_sys(
            retriever, timestamp, system, remove_failed, suite, task, variant
        )
        coverage_matrix[timestamp][i].update(feats)
        for feature in qf.KNOWN_FEATURES:
            matrix[i][feature] = len(feats[feature])

    return columns, matrix


@app.callback(
    Output("cell-data-container", "children"),
    Input("coverage-matrix-table", "active_cell"),
    State("coverage-matrix-table", "derived_viewport_data"),
    State({"type": "timestamp-dropdown", "index": 4}, "value"),
)
def display_cell_data(active_cell, table_data, timestamp):
    if not active_cell or not table_data or not timestamp:
        return "Click on a cell to see feature data"

    row_idx = active_cell["row"]
    col_idx = active_cell["column_id"]

    # Get the system name from the row data
    system = table_data[row_idx]["system"]

    system_data = next(
        (item for item in coverage_matrix[timestamp] if item["system"] == system), None
    )
    if not system_data:
        return "No data found for the selected cell."

    features = system_data[col_idx]
    if len(features) == 0:
        return "No data found for the selected cell."

    table = dash_table.DataTable(
        id="coverage-feature-table",
        data=make_dict_table_friendly(features),
        columns=get_columns_from_list_of_dicts(features),
        filter_action="native",
        sort_action="native",
        style_cell={
            "textAlign": "center",
            "maxWidth": "100%",
            "whiteSpace": "normal",
        },
        style_table={"overflowX": "auto", "maxWidth": "100%", "margin": "auto"},
    )
    return html.Div(
        [html.H4(f"{system} ---- {col_idx}:", style={"textAlign": "center"}), table],
        style={"maxWidth": "100%", "margin": "auto"},
    )



@app.callback(
    Output("coverage-modal-body", "children"),
    Output("coverage-modal", "is_open"),
    Input("coverage-feature-table", "active_cell"),
    State("coverage-feature-table", "derived_viewport_data"),
    State("coverage-matrix-table", "active_cell"),
    State("coverage-matrix-table", "derived_viewport_data"),
    State({"type": "timestamp-dropdown", "index": 4}, "value"),
)
def populate_tests_in_coverage_diff_cmds(active_cell, table_data, system_active_cell, system_table_data, timestamp):
    if not active_cell:
        raise dash.exceptions.PreventUpdate

    # Get the system name from the system matrix data
    system = system_table_data[system_active_cell["row"]]["system"]

    row_idx = active_cell["row"]
    feature = copy.deepcopy(table_data[row_idx])
    for k, v in feature.items():
        try:
            feature[k] = json.loads(v)
        except:
            pass

    results = qf.find_feat(retriever, timestamp, feature, remove_failed=False, system=system, force_exact=True)
    table = dash_table.DataTable(
        data=get_task_list_from_dict(results),
        columns=[{"name":"suite","id":"suite"}, {"name":"test","id":"test"}, {"name":"variant","id":"variant"}],
        filter_action="native",
        sort_action="native",
        style_cell={
            "textAlign": "center",
            "maxWidth": "100%",
            "whiteSpace": "normal",
        },
        style_table={"overflowX": "auto", "maxWidth": "100%", "minWidth": "600px", "margin": "auto"},
    )
    return html.Div(
        [html.H4(f"{system} --- {feature}", style={"textAlign": "center"}), table],
        style={"maxWidth": "100%", "margin": "auto"},
    ), True


@app.callback(
    Output("duplicate-table", "columns"),
    Output("duplicate-table", "data"),
    Input({"type": "timestamp-dropdown", "index": 5}, "value"),
    Input({"type": "systems-dropdown", "index": 5}, "value"),
)
def calculate_duplicate_systems(timestamp, system):

    if not timestamp or not system:
        return [], []

    if timestamp in cached_duplicates and system in cached_duplicates[timestamp]:
        duplicates = cached_duplicates[timestamp][system]
    else:
        duplicates = qf.dup(retriever, timestamp, system, False)
        if timestamp in cached_duplicates:
            cached_duplicates[timestamp][system] = duplicates
        else:
            cached_duplicates[timestamp] = {system: duplicates}

    columns = [
        {"name": "suite", "id": "suite"},
        {"name": "task", "id": "task"},
        {"name": "variant", "id": "variant"},
    ]

    rows = [
        {"suite": d.suite, "task": d.task_name, "variant": d.variant}
        for d in duplicates
    ]

    return columns, rows


@app.callback(
    Output("cell-duplicate-container", "children"),
    Input("duplicate-table", "active_cell"),
    State("duplicate-table", "derived_viewport_data"),
    State({"type": "timestamp-dropdown", "index": 5}, "value"),
    State({"type": "systems-dropdown", "index": 5}, "value"),
)
def display_duplicate_cell_data(active_cell, table_data, timestamp, system):
    if not active_cell or not table_data or not timestamp:
        return "Click on a cell to see feature data"

    row_idx = active_cell["row"]

    test = table_data[row_idx]

    features = qf.feat_sys(
        retriever,
        timestamp,
        system,
        False,
        suite=test["suite"],
        task=test["task"],
        variant=test["variant"],
    )

    tables = [
        html.H4(
            f"{system}:{str(qf.TaskIdVariant(test['suite'], test['task'], test['variant']))}",
            style={"textAlign": "center"},
        )
    ]
    for feature_name, feature_data in features.items():
        processed = []
        for feature in feature_data:
            feat_dict = {}
            for k, v in feature.items():
                feat_dict[k] = json.dumps(v) if isinstance(v, list) else v
            processed.append(feat_dict)
        table = dash_table.DataTable(
            data=processed,
            columns=get_columns_from_list_of_dicts(feature_data),
            filter_action="native",
            sort_action="native",
            style_cell={
                "textAlign": "center",
                "maxWidth": "100%",
                "whiteSpace": "normal",
            },
            style_table={"overflowX": "auto", "maxWidth": "100%", "margin": "auto"},
        )
        tables.append(
            html.Div(
                [html.H4(feature_name), table],
                style={"maxWidth": "100%", "margin": "auto"},
            )
        )

    return tables


def get_task_list_from_dict(tests: dict[str, qf.TaskIdVariant]) -> list[dict]:
    processed = []
    for _, test_list in tests.items():
        for test in test_list:
            d = {"suite": test.suite, "test": test.task_name, "variant": test.variant}
            if d not in processed:
                processed.append(d)
    return processed


@app.callback(
    Output("explore-by-feature-table", "columns"),
    Output("explore-by-feature-table", "data"),
    Input({"type": "timestamp-dropdown", "index": 6}, "value"),
    Input("features-dropdown", "value"),
    Input("add-frequency-numbers-switch", "on"),
)
def populate_feature_table(timestamp, selected_feature, add_freq):
    if not timestamp or not selected_feature:
        return [], []

    if (
        add_freq
        and timestamp in cached_feat_explore
        and selected_feature in cached_feat_explore[timestamp]
    ):
        return (
            cached_feat_explore[timestamp][selected_feature]["cols"],
            cached_feat_explore[timestamp][selected_feature]["processed"],
        )

    features = retriever.get_all_features(timestamp)
    feature_data = features[selected_feature]
    processed = []
    for feature in feature_data:
        feat_dict = {}
        if add_freq:
            feat_dict["number-of-tests-with-feature"] = len(
                get_task_list_from_dict(
                    qf.find_feat(retriever, timestamp, feature, False)
                )
            )
        for k, v in feature.items():
            feat_dict[k] = json.dumps(v) if isinstance(v, list) else v
        processed.append(feat_dict)
    cols = get_columns_from_list_of_dicts(feature_data)
    if add_freq:
        cols = [{"name": "Number of tests with feature", "id": "number-of-tests-with-feature"}] + cols

    if add_freq:
        if timestamp not in cached_feat_explore:
            cached_feat_explore[timestamp] = {}
        cached_feat_explore[timestamp][selected_feature] = {
            "cols": cols,
            "processed": processed,
        }
    return cols, processed


@app.callback(
    Output("explore-by-feature-data-container", "children"),
    Input("explore-by-feature-table", "active_cell"),
    State("explore-by-feature-table", "derived_viewport_data"),
    State({"type": "timestamp-dropdown", "index": 6}, "value"),
    State("features-dropdown", "value"),
)
def update_test_list(active_cell, table_data, timestamp, selected_feature):
    if not active_cell or not table_data or not timestamp or not selected_feature:
        return "Click on a cell to see tests"

    row_idx = active_cell["row"]

    feature = copy.deepcopy(table_data[row_idx])
    if "number-of-tests-with-feature" in feature:
        del feature["number-of-tests-with-feature"]

    tests = qf.find_feat(retriever, timestamp, feature, False)

    processed = []
    sys_dict = {}
    for sys, test_list in tests.items():
        for test in test_list:
            d = {"suite": test.suite, "test": test.task_name, "variant": test.variant}
            s = json.dumps(d)
            if s not in sys_dict:
                sys_dict[s] = []
            sys_dict[s].append(sys)
            if d not in processed:
                processed.append(d)

    for p in processed:
        s = json.dumps(p)
        p["systems"] = "\n".join(sorted(sys_dict[s]))
        p["systems"] = p["systems"].replace('-', '_')

    return [
        html.H4(f"Tests that contain feature {feature}"),
        dash_table.DataTable(
            data=processed,
            columns=[
                {"name": "suite", "id": "suite"},
                {"name": "test", "id": "test"},
                {"name": "variant", "id": "variant"},
                {"name": "systems", "id": "systems"},
            ],
            filter_action="native",
            sort_action="native",
            style_cell={
                "textAlign": "center",
                "maxWidth": "auto",
                "whiteSpace": "normal",
            },
            style_data_conditional=[
                {
                    "if": {"column_id": "systems"},
                    "whiteSpace": "pre-line",
                    "maxWidth": "auto",
                    "width": "auto",
                }
            ],
            style_table={"overflowX": "auto", "maxWidth": "100%", "margin": "auto"},
        ),
    ]