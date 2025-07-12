import json
import os
import sys
import tempfile

# To ensure the unit test can be run from any point in the filesystem,
# add parent folder to path to permit relative imports
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

import feattranslator
from query_features import ALL_FEATURES_FILE
from features import Cmd, Endpoint, Change, Task, Ensure, Interface, Status

class TestMongoUpload:

    def test_all_features_translation(self):
        data = {
            "changes": ["pre-download"],
            "commands": ["abort","debug execution apparmor"],
            "endpoints": [
                {"method": "GET","path": "/v2/system-info"},
                {
                    "actions": ["advise-system-key-mismatch"],
                    "method": "POST",
                    "path": "/v2/system-info"
                },
                {
                    "actions": ["connect","disconnect"],
                    "method": "POST",
                    "path": "/v2/interfaces"
                },
            ],
            "ensures": [
                {"function": "ensureAliasesV2","manager": "SnapManager"},
                {"function": "autoRefresh.ensureLastRefreshAnchor","manager": "SnapManager"},
            ],
            "interfaces": ["accel"],
            "tasks": [
                {"kind": "hotplug-add-slot"},
                {"has-undo": True,"kind": "migrate-snap-home"},
            ]
        }
        with tempfile.TemporaryDirectory() as tmpdir:
            with open(os.path.join(tmpdir, ALL_FEATURES_FILE), 'w', encoding='utf-8') as f:
                json.dump(data, f)
            feattranslator.translate_all_features(os.path.join(tmpdir, ALL_FEATURES_FILE), os.path.join(tmpdir, "my-output.json"))
            with open(os.path.join(tmpdir, "my-output.json"), 'r', encoding='utf-8') as f:
                rewritten = json.load(f)
                assert "changes" in rewritten
                assert len(rewritten["changes"]) == 1
                assert rewritten["changes"][0] == Change(kind=data["changes"][0])
                assert "cmds" in rewritten
                assert len(rewritten["cmds"]) == 2
                assert Cmd(cmd=data["commands"][0]) in rewritten["cmds"]
                assert Cmd(cmd=data["commands"][1]) in rewritten["cmds"]
                assert "endpoints" in rewritten
                assert len(rewritten["endpoints"]) == 4
                for ep in data["endpoints"]:
                    if "actions" in ep:
                        for act in ep["actions"]:
                            assert Endpoint(method=ep["method"], path=ep["path"], action=act)
                    else:
                        assert Endpoint(method=ep["method"], path=ep["path"]) in rewritten["endpoints"]
                assert "ensures" in rewritten
                assert len(rewritten["ensures"]) == 2
                for en in data["ensures"]:
                    assert Ensure(manager=en["manager"], function=en["function"]) in rewritten["ensures"]
                assert "interfaces" in rewritten
                assert len(rewritten["interfaces"]) == 1
                assert Interface(name=data["interfaces"][0]) in rewritten["interfaces"]
                assert "tasks" in rewritten
                assert len(rewritten["tasks"]) == 5
                for status in [Status.done, Status.error, Status.undone]:
                    assert Task(kind=data["tasks"][0]["kind"], last_status=status)
                for status in [Status.done, Status.error]:
                    assert Task(kind=data["tasks"][1]["kind"], last_status=status)



