package main

func commandFlagHelp(args []string, commands ...string) string {
	for index := 0; index+1 < len(args); index++ {
		next := args[index+1]
		if next != "--help" && next != "-h" && next != "help" {
			continue
		}
		for _, command := range commands {
			if args[index] == "--"+command {
				return command
			}
		}
	}
	return ""
}
