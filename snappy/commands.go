package snappy

// register all dispatcher functions
func registerCommands() {
	registerCommand("build", "build a snap package", cmdBuild)
	registerCommand("install", "install a snap package", cmdInstall)
	registerCommand("search", "search for snap packages", cmdSearch)
	registerCommand("update", "update installed parts", cmdUpdate)
	registerCommand("list", "display versions of installed parts", cmdList)
}
