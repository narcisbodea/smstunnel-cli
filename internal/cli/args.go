package cli

import "strings"

type optionSpec struct {
	name   string
	isBool bool
	short  byte // 'h' for -h, 0 for none
}

// option is a parsed option: true for a flag, or its value.
type option struct {
	isBool bool
	value  string
}

// parsedArgs is what parseArgs gives: positionals, and the options in the order they first appeared
// (the order of a JavaScript object's keys, which the "not an option of" check walks).
type parsedArgs struct {
	positionals []string
	options     map[string]option
	order       []string
}

func (p *parsedArgs) has(name string) bool { _, ok := p.options[name]; return ok }

func (p *parsedArgs) flag(name string) bool {
	o, ok := p.options[name]
	return ok && o.isBool
}

// str returns a string option's value and whether it was given.
func (p *parsedArgs) str(name string) (string, bool) {
	o, ok := p.options[name]
	if !ok || o.isBool {
		return "", false
	}
	return o.value, true
}

func (p *parsedArgs) set(name string, o option) {
	if _, ok := p.options[name]; !ok {
		p.order = append(p.order, name)
	}
	p.options[name] = o
}

// parseArgs is the Node version's parser: --name value, --name=value, flags, the listed short forms, and "--"
// after which everything is a positional. A word starting with one "-" that is not a listed short form stays a
// positional, so a text such as "-20% today" needs no quoting tricks. Unknown --options are refused.
func parseArgs(argv []string, specs []optionSpec) (*parsedArgs, error) {
	byName := map[string]optionSpec{}
	byShort := map[byte]optionSpec{}
	for _, s := range specs {
		byName[s.name] = s
		if s.short != 0 {
			byShort[s.short] = s
		}
	}
	p := &parsedArgs{options: map[string]option{}}
	for i := 0; i < len(argv); i++ {
		token := argv[i]
		if token == "--" {
			p.positionals = append(p.positionals, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(token, "--") && len(token) > 2 {
			eq := strings.IndexByte(token, '=')
			name := token[2:]
			if eq != -1 {
				name = token[2:eq]
			}
			spec, ok := byName[name]
			if !ok {
				return nil, usageError(`Unknown option --` + name + `. See "smstunnel --help".`)
			}
			if spec.isBool {
				if eq != -1 {
					return nil, usageError("--" + name + " takes no value.")
				}
				p.set(name, option{isBool: true})
				continue
			}
			var value string
			if eq != -1 {
				value = token[eq+1:]
			} else {
				if i+1 >= len(argv) || (strings.HasPrefix(argv[i+1], "--") && len(argv[i+1]) > 2) {
					return nil, usageError("--" + name + " needs a value.")
				}
				value = argv[i+1]
				i++
			}
			if value == "" {
				return nil, usageError("--" + name + " needs a value.")
			}
			p.set(name, option{value: value})
			continue
		}
		if len(token) == 2 && token[0] == '-' && isASCIILetter(token[1]) {
			if spec, ok := byShort[token[1]]; ok {
				if spec.isBool {
					p.set(spec.name, option{isBool: true})
					continue
				}
				if i+1 >= len(argv) {
					return nil, usageError("-" + string(spec.short) + " needs a value.")
				}
				p.set(spec.name, option{value: argv[i+1]})
				i++
				continue
			}
		}
		p.positionals = append(p.positionals, token)
	}
	return p, nil
}

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
