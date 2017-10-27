#! /usr/bin/python
 
from gi.repository import Gio
 
class Example:
 
    def __init__(self):
        setting = Gio.Settings.new("org.example.myapp")
        print(setting)
 
 
if __name__ == "__main__":
    Example()