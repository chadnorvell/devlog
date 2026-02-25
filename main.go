package main

import "os"

func main() {
	if len(os.Args) < 2 {
		cmdNote()
		return
	}
	switch os.Args[1] {
	case "gen":
		cmdGen()
	case "gen-prompt":
		cmdGenPrompt()
	case "watch":
		cmdWatch()
	case "unwatch":
		cmdUnwatch()
	case "start":
		cmdStart()
	case "stop":
		cmdStop()
	case "status":
		cmdStatus()
	default:
		cmdNote()
	}
}
