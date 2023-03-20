#!/usr/bin/python3 -u

import gi
from gi.repository import GLib

import dbus
import dbus.service
import dbus.mainloop.glib

dbus.mainloop.glib.DBusGMainLoop(set_as_default=True)

AA_PROMPT_IFACE = "io.snapcraft.AppArmorPrompt"
AA_PROMPT_PATH = "/io/snapcraft/AppArmorPrompt"


AGENT_INTERFACE = "io.snapcraft.PromptAgent"

class FlipFlopAgent(dbus.service.Object):

    def __init__(self, *args, **kwargs):
        dbus.service.Object.__init__(self, *args, **kwargs)
        self.answer = True
    
    @dbus.service.method(AGENT_INTERFACE, in_signature="sa{ss}", out_signature="ba{ss}")
    def Prompt(self, prompt_path, info):
        #app = info["label"]
        #icon = info["icon"]
        #operation = info["operation"]
        prompt_answer = self.answer
        print(f"Prompt() {prompt_path} {info} will reply {prompt_answer}")
        # flip/flop for next time
        self.answer = not self.answer
        
        return prompt_answer, {"extra": "info"}


def main():
    # create agent prompt
    buspath = "/io/snapcraft/PromptAgent"
    bus = dbus.SystemBus()
    agent = FlipFlopAgent(bus, buspath)

    # register to aa-listen
    obj = bus.get_object(AA_PROMPT_IFACE, AA_PROMPT_PATH)
    obj.RegisterAgent(buspath)
    print("fake agent registered")

    loop = GLib.MainLoop()
    loop.run()


if __name__ == "__main__":
    main()
