package analyze

import "strings"

// formatJVMMethodSymbol converts a JVM method descriptor into a source-like Java signature.
// Unknown or incomplete symbols are preserved so diagnostics never lose the original evidence.
func formatJVMMethodSymbol(symbol string) string {
	symbol = strings.TrimSpace(symbol)
	ownerEnd := strings.Index(symbol, ";->")
	if ownerEnd < 0 || !strings.HasPrefix(symbol, "L") {
		return symbol
	}

	owner, next, ok := parseJVMType(symbol, 0)
	if !ok || next+2 > len(symbol) || symbol[next:next+2] != "->" {
		return symbol
	}
	methodDescriptor := symbol[next+2:]
	argumentsStart := strings.IndexByte(methodDescriptor, '(')
	argumentsEnd := strings.IndexByte(methodDescriptor, ')')
	if argumentsStart <= 0 || argumentsEnd < argumentsStart {
		return symbol
	}

	methodName := methodDescriptor[:argumentsStart]
	arguments, ok := parseJVMArguments(methodDescriptor[argumentsStart+1 : argumentsEnd])
	if !ok {
		return symbol
	}
	returnType, consumed, ok := parseJVMType(methodDescriptor, argumentsEnd+1)
	if !ok || consumed != len(methodDescriptor) {
		return symbol
	}

	if methodName == "<init>" {
		return owner + "(" + strings.Join(arguments, ", ") + ")"
	}
	if methodName == "<clinit>" {
		return owner + ".<инициализация класса>()"
	}
	return owner + "." + methodName + "(" + strings.Join(arguments, ", ") + "): " + returnType
}

func parseJVMArguments(descriptor string) ([]string, bool) {
	arguments := make([]string, 0, 4)
	for offset := 0; offset < len(descriptor); {
		argument, next, ok := parseJVMType(descriptor, offset)
		if !ok || argument == "void" || next <= offset {
			return nil, false
		}
		arguments = append(arguments, argument)
		offset = next
	}
	return arguments, true
}

func parseJVMType(descriptor string, offset int) (string, int, bool) {
	if offset < 0 || offset >= len(descriptor) {
		return "", offset, false
	}
	arrays := 0
	for offset < len(descriptor) && descriptor[offset] == '[' {
		arrays++
		offset++
	}
	if offset >= len(descriptor) {
		return "", offset, false
	}

	var value string
	switch descriptor[offset] {
	case 'V':
		value = "void"
		offset++
	case 'Z':
		value = "boolean"
		offset++
	case 'B':
		value = "byte"
		offset++
	case 'C':
		value = "char"
		offset++
	case 'S':
		value = "short"
		offset++
	case 'I':
		value = "int"
		offset++
	case 'J':
		value = "long"
		offset++
	case 'F':
		value = "float"
		offset++
	case 'D':
		value = "double"
		offset++
	case 'L':
		end := strings.IndexByte(descriptor[offset:], ';')
		if end < 0 {
			return "", offset, false
		}
		end += offset
		value = strings.ReplaceAll(descriptor[offset+1:end], "/", ".")
		if value == "" {
			return "", offset, false
		}
		offset = end + 1
	default:
		return "", offset, false
	}
	if arrays > 0 {
		if value == "void" {
			return "", offset, false
		}
		value += strings.Repeat("[]", arrays)
	}
	return value, offset, true
}
