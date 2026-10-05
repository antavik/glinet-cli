package cmd

import "flag"

type Args struct {
	Sub string   // subcommand; "" when absent
	Pos []string // operands after Sub
}

func ParseArgs(fs *flag.FlagSet, args []string) (Args, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return Args{}, err
		}
		if fs.NArg() == 0 {
			break
		}
		pos, args = append(pos, fs.Arg(0)), fs.Args()[1:]
	}
	var a Args
	if len(pos) > 0 {
		a.Sub, a.Pos = pos[0], pos[1:]
	}
	return a, nil
}
