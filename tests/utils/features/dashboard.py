#!/usr/bin/env python3

from dash import html, dcc, dash_table
import dash_bootstrap_components as dbc
import dash_daq as daq
import os
import sys

# To ensure this can be run from any point in the filesystem,
# add parent folder to path to permit relative imports
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

from dashboard_callbacks import *

app.layout = html.Div(
    style={
        "padding": "15px",
    },
    children=[
        html.Div(children="", style={"fontSize": "24px", "marginBottom": "10px"}),
        html.H4(
            "1. First input path for either a mongo credential file or a folder containing the data."
        ),
        dcc.Loading(
            html.Div(
                [
                    dcc.Input(
                        id="input-path",
                        type="text",
                        placeholder="full path to mongo cred file or to folder with data.",
                    ),
                    dbc.Button("Set data", id="set-data-button"),
                    html.Div(id="empty-div", style={"display": "none"}),
                ]
            ),
        ),
        dcc.ConfirmDialog(
            id="error-with-retriever",
            message="The indicated path does not exist",
            displayed=False,
        ),
        html.Div(children="", style={"fontSize": "24px", "marginBottom": "20px"}),
        html.H4("2. Explore data"),
        html.Div(children="", style={"fontSize": "24px", "marginBottom": "20px"}),
        dbc.Button(
            "Systems coverage vs. all features",
            id={"type": "toggle-button", "index": 1},
            n_clicks=0,
            className="mb-3",
            disabled=True,
        ),
        dbc.Collapse(
            html.Div(
                [
                    html.H4(
                        "Calculates the difference between all features and each system's features"
                    ),
                    html.H5("Calculated as follows:"),
                    html.H6(
                        "commands, ensures, endpoints: the entire feature data is used for equality check"
                    ),
                    html.H6(
                        "tasks: only kind and last_status are used for equality check"
                    ),
                    html.H6("changes: only kind is used for equality check"),
                    html.H6("interfaces: only name is used for equality check"),
                    dcc.Dropdown(
                        id={"type": "timestamp-dropdown", "index": 1},
                        options=timestamp_options,
                        placeholder="Select timestamp",
                    ),
                    dcc.Loading(
                        html.Div(
                            [
                                dash_table.DataTable(
                                    id="all-features-diff-table",
                                    columns=[],
                                    data=[],
                                    filter_action="native",
                                    sort_action="native",
                                    active_cell=None,
                                    style_cell={
                                        "textAlign": "center",
                                        "minWidth": "100px",
                                        "maxWidth": "200px",
                                        "whiteSpace": "normal",
                                    },
                                    style_table={
                                        "overflowX": "auto",
                                        "maxWidth": "900px",
                                        "margin": "auto",
                                    },
                                ),
                                html.Div(
                                    id="all-features-diff-cell-data-container",
                                    style={
                                        "display": "inline-block",
                                        "marginLeft": "20px",
                                        "verticalAlign": "top",
                                        "flex": 1,
                                        "overflow": "auto",
                                        "maxWidth": "900px",
                                    },
                                ),
                            ],
                            id="all-features-diff-container",
                            style={
                                "display": "flex",
                                "justifyContent": "center",
                                "alignItems": "flex-start",
                                "gap": "20px",
                                "maxWidth": "1900px",
                                "margin": "auto",
                            },
                        ),
                    ),
                ]
            ),
            id={"type": "collapse", "index": 1},
            is_open=False,
        ),
        html.Div(children="", style={"fontSize": "24px", "marginBottom": "20px"}),
        dbc.Button(
            "Compare feature coverage between systems",
            id={"type": "toggle-button", "index": 2},
            n_clicks=0,
            className="mb-3",
            disabled=True,
        ),
        dbc.Collapse(
            html.Div(
                [
                    html.Div(
                        children="Calculates difference in coverage between set(system_1_features) - set(system_2_features)",
                        style={"fontSize": "20px", "marginBottom": "20px"},
                    ),
                    html.Div(
                        [
                            html.Div(
                                children="System 1", style={"marginBottom": "20px"}
                            ),
                            dcc.Dropdown(
                                id={"type": "timestamp-dropdown", "index": 2},
                                options=timestamp_options,
                                placeholder="Select timestamp",
                            ),
                            dcc.Dropdown(
                                id={"type": "systems-dropdown", "index": 2},
                                options=[],
                                placeholder="Select system",
                            ),
                            dcc.Input(
                                id={"type": "systems-regex-input", "index": 2},
                                type="text",
                                placeholder="Use regex here to select systems instead of dropdown",
                                debounce=True,
                            ),
                            html.Div(children="", style={"marginBottom": "10px"}),
                            html.Button(
                                "🔄",
                                id="switch-button",
                                n_clicks=0,
                                style={
                                    "fontSize": "20px",
                                    "padding": "4px 8px",
                                    "margin": "0 10px",
                                    "borderRadius": "50%",
                                    "border": "1px solid #ccc",
                                    "backgroundColor": "white",
                                    "cursor": "pointer",
                                    "lineHeight": "1",
                                    "display": "inline-flex",
                                    "alignItems": "center",
                                    "justifyContent": "center",
                                    "width": "32px",
                                    "height": "32px",
                                },
                            ),
                            html.Div(
                                children="System 2",
                                style={"marginTop": "10px", "marginBottom": "20px"},
                            ),
                            dcc.Dropdown(
                                id={"type": "timestamp-dropdown", "index": 3},
                                options=timestamp_options,
                                placeholder="Select timestamp",
                            ),
                            dcc.Dropdown(
                                id={"type": "systems-dropdown", "index": 3},
                                options=[],
                                placeholder="Select system",
                            ),
                            daq.BooleanSwitch(
                                id="remove-failed-switch",
                                label="Remove failed tests",
                            ),
                            daq.BooleanSwitch(
                                id="only-same-switch",
                                label="Only compare features across tests that are present in both systems",
                            ),
                            daq.BooleanSwitch(
                                id="match-snap-types",
                                label="Match snap types when comparing features",
                                disabled=False
                            ),
                        ],
                        style={"width": "25%"},
                    ),
                    dcc.Loading(
                        html.Div(id="coverage-diff-container"),
                    ),
                    dbc.Modal(
                        [
                            dbc.ModalHeader(dbc.ModalTitle("Tests with feature")),
                            dbc.ModalBody(id="coverage-diff-modal-body"),
                        ],
                        id="coverage-diff-modal",
                        size="xl",
                        is_open=False,
                    ),
                ]
            ),
            id={"type": "collapse", "index": 2},
            is_open=False,
        ),
        html.Div(children="", style={"fontSize": "24px", "marginBottom": "20px"}),
        dbc.Button(
            "Feature coverage matrix per system",
            id={"type": "toggle-button", "index": 3},
            n_clicks=0,
            className="mb-3",
            disabled=True,
        ),
        dbc.Collapse(
            html.Div(
                [
                    html.Div(
                        children="Calculates the feature coverage matrix for all systems",
                        style={"fontSize": "20px", "marginBottom": "20px"},
                    ),
                    html.Div(children="Timestamp", style={"marginBottom": "20px"}),
                    html.Div(
                        [
                            dcc.Dropdown(
                                id={"type": "timestamp-dropdown", "index": 4},
                                options=timestamp_options,
                                placeholder="Select timestamp",
                            ),
                            html.Div(
                                children="coverage data filter",
                                style={
                                    "fontSize": "16px",
                                    "marginTop": "20px",
                                    "marginBottom": "20px",
                                },
                            ),
                            dcc.Dropdown(
                                id="suite-dropdown-filter",
                                options=[],
                                placeholder="Select suite",
                                clearable=True,
                            ),
                            dcc.Dropdown(
                                id="task-dropdown-filter",
                                options=[],
                                placeholder="Select task",
                                clearable=True,
                            ),
                            dcc.Dropdown(
                                id="variant-dropdown-filter",
                                options=[],
                                placeholder="Select variant",
                                clearable=True,
                            ),
                            daq.BooleanSwitch(
                                id="coverage-remove-failed-switch",
                                label="Remove failed tests",
                            ),
                        ],
                        style={"width": "25%"},
                    ),
                    dcc.Loading(
                        html.Div(
                            [
                                dash_table.DataTable(
                                    id="coverage-matrix-table",
                                    filter_action="native",
                                    sort_action="native",
                                    style_cell={
                                        "textAlign": "center",
                                        "minWidth": "100px",
                                        "maxWidth": "200px",
                                        "whiteSpace": "normal",
                                    },
                                    style_table={
                                        "overflowX": "auto",
                                        "maxWidth": "900px",
                                        "margin": "auto",
                                    },
                                ),
                                html.Div(
                                    id="cell-data-container",
                                    style={
                                        "display": "inline-block",
                                        "marginLeft": "20px",
                                        "verticalAlign": "top",
                                        "flex": 1,
                                        "overflow": "auto",
                                        "maxWidth": "900px",
                                    },
                                ),
                            ],
                            id="coverage-matrix-container",
                            style={
                                "display": "flex",
                                "justifyContent": "center",
                                "alignItems": "flex-start",
                                "gap": "20px",
                                "maxWidth": "1900px",
                                "margin": "auto",
                            },
                        ),
                    ),
                    dbc.Modal(
                        [
                            dbc.ModalHeader(dbc.ModalTitle("Tests with feature")),
                            dbc.ModalBody(id="coverage-modal-body"),
                        ],
                        id="coverage-modal",
                        size="xl",
                        is_open=False,
                    ),
                ]
            ),
            id={"type": "collapse", "index": 3},
            is_open=False,
        ),
        html.Div(children="", style={"fontSize": "24px", "marginBottom": "20px"}),
        dbc.Button(
            "Duplicate features",
            id={"type": "toggle-button", "index": 4},
            n_clicks=0,
            className="mb-3",
            disabled=True,
        ),
        dbc.Collapse(
            html.Div(
                [
                    html.Div(children="Timestamp", style={"marginBottom": "20px"}),
                    dcc.Dropdown(
                        id={"type": "timestamp-dropdown", "index": 5},
                        options=timestamp_options,
                        placeholder="Select timestamp",
                    ),
                    dcc.Dropdown(
                        id={"type": "systems-dropdown", "index": 5},
                        options=[],
                        placeholder="Select system",
                    ),
                    html.Div(
                        [
                            dcc.Loading(
                                dash_table.DataTable(
                                    id="duplicate-table",
                                    filter_action="native",
                                    sort_action="native",
                                    style_cell={
                                        "textAlign": "center",
                                        "minWidth": "100px",
                                        "maxWidth": "200px",
                                        "whiteSpace": "normal",
                                    },
                                    style_table={
                                        "overflowX": "auto",
                                        "maxWidth": "900px",
                                        "margin": "auto",
                                    },
                                ),
                            ),
                            html.Div(
                                id="cell-duplicate-container",
                                style={
                                    "display": "inline-block",
                                    "marginLeft": "20px",
                                    "verticalAlign": "top",
                                    "flex": 1,
                                    "overflow": "auto",
                                    "maxWidth": "900px",
                                },
                            ),
                        ],
                        id="duplicate-matrix-container",
                        style={
                            "display": "flex",
                            "justifyContent": "center",
                            "alignItems": "flex-start",
                            "gap": "20px",
                            "maxWidth": "1900px",
                            "margin": "auto",
                        },
                    ),
                ]
            ),
            id={"type": "collapse", "index": 4},
            is_open=False,
        ),
        html.Div(children="", style={"fontSize": "24px", "marginBottom": "20px"}),
        dbc.Button(
            "Explore by feature",
            id={"type": "toggle-button", "index": 5},
            n_clicks=0,
            className="mb-3",
            disabled=True,
        ),
        dbc.Collapse(
            html.Div(
                [
                    html.Div(children="Timestamp", style={"marginBottom": "20px"}),
                    dcc.Dropdown(
                        id={"type": "timestamp-dropdown", "index": 6},
                        options=timestamp_options,
                        placeholder="Select timestamp",
                    ),
                    dcc.Dropdown(
                        id="features-dropdown",
                        options=feature_options,
                        placeholder="Select feature",
                    ),
                    daq.BooleanSwitch(
                        id="add-frequency-numbers-switch",
                        label="Add column with number of tests that have feature",
                        on=True
                    ),
                    html.Div(
                        [
                            dcc.Loading(
                                dash_table.DataTable(
                                    id="explore-by-feature-table",
                                    filter_action="native",
                                    sort_action="native",
                                    style_cell={
                                        "textAlign": "center",
                                        "minWidth": "100px",
                                        "maxWidth": "500px",
                                        "whiteSpace": "normal",
                                    },
                                    style_table={
                                        "overflowX": "auto",
                                        "maxWidth": "900px",
                                        "margin": "auto",
                                    },
                                ),
                            ),
                            html.Div(
                                id="explore-by-feature-data-container",
                                style={
                                    "display": "inline-block",
                                    "marginLeft": "20px",
                                    "verticalAlign": "top",
                                    "flex": 1,
                                    "overflow": "auto",
                                    "maxWidth": "900px",
                                },
                            ),
                        ],
                        id="explore-by-feature-container",
                        style={
                            "display": "flex",
                            "justifyContent": "center",
                            "alignItems": "flex-start",
                            "gap": "20px",
                            "maxWidth": "1900px",
                            "margin": "auto",
                        },
                    ),
                ]
            ),
            id={"type": "collapse", "index": 5},
            is_open=False,
        ),
    ]
)




if __name__ == "__main__":
    app.run(debug=False)
