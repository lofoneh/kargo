package yaml

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/akuity/kargo/pkg/sjson"
)

// The maximum size of the input buffer for processing YAML files. Given that the max size of a
// Kubernetes manifest (the most common thing we're dealing with) is 1 MB, this number is chosen to
// assume that the max value of any given key would be just shy of that number (imagine a config map
// with a single large value which is a config file for something in the container). Please note
// that we do not allocate a buffer of this size up front; this is just the maximum size that we
// will allow when processing input.
const maxBufferSize = 1024 * 1024 // 1 MB

// Update represents a discrete update to be made to a YAML document.
type Update struct {
	// Key is the dot-separated path to the field to update.
	Key string
	// Value is the new value to set for the field.
	Value any
}

// SetValuesInFile overwrites the specified file with the changes specified by
// the list of Updates. Keys are of the form <key 0>.<key 1>...<key n>.
// Integers may be used as keys in cases where a specific node needs to be
// selected from a sequence. An error is returned for any attempted update to a
// key that does not exist or does not address a scalar node. Importantly, all
// comments and style choices in the input bytes are preserved in the output.
func SetValuesInFile(file string, updates []Update) error {
	inBytes, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf(
			"error reading file %q: %w",
			file,
			err,
		)
	}
	outBytes, err := SetValuesInBytes(inBytes, updates)
	if err != nil {
		return fmt.Errorf("error mutating bytes: %w", err)
	}

	// This file should always exist already, so the permissions we choose here
	// don't really matter. (They only matter when this function call creates
	// the file, which it never will.) We went with 0600 just to appease the
	// gosec linter.
	if err = os.WriteFile(file, outBytes, 0600); err != nil {
		return fmt.Errorf(
			"error writing mutated bytes to file %q: %w",
			file,
			err,
		)
	}
	return nil
}

// SetValuesInBytes returns a copy of the provided bytes with the changes
// specified by Updates applied. Keys are of the form <key 0>.<key 1>...<key n>.
// Integers may be used as keys in cases where a specific node needs to be
// selected from a sequence. An error is returned for any attempted update to a
// key that does not exist or does not address a scalar node. Importantly, all
// comments and style choices in the input bytes are preserved in the output.
func SetValuesInBytes(inBytes []byte, updates []Update) ([]byte, error) {
	doc := &yaml.Node{}
	if err := yaml.Unmarshal(inBytes, doc); err != nil {
		return nil, fmt.Errorf("error unmarshaling input: %w", err)
	}

	// Scalars to replace are grouped by the line they begin on, since a line
	// can hold more than one of them. If a scalar is updated more than once, the
	// last update wins.
	valuesByNode := map[*yaml.Node]any{}
	nodesByLine := map[int][]*yaml.Node{}
	for _, update := range updates {
		keyPath, err := sjson.SplitKey(update.Key)
		if err != nil {
			return nil, fmt.Errorf("error splitting key %s: %w", update.Key, err)
		}
		node, err := findScalarNode(doc, keyPath)
		if err != nil {
			return nil, fmt.Errorf("error finding key %s: %w", update.Key, err)
		}
		if _, found := valuesByNode[node]; !found {
			nodesByLine[node.Line-1] = append(nodesByLine[node.Line-1], node)
		}
		valuesByNode[node] = update.Value
	}

	outBuf := &bytes.Buffer{}

	scanner := bufio.NewScanner(bytes.NewBuffer(inBytes))
	scanner.Split(bufio.ScanLines)
	// Create an initial buffer of 100B which should be plenty for most lines. This means we will
	// likely avoid an allocation on the first scan. If we encounter a line that exceeds this size,
	// the buffer will grow up to maxBufferSize.
	var buf = make([]byte, 0, 100)
	scanner.Buffer(buf, maxBufferSize)
	var line int
	for scanner.Scan() {
		const errMsg = "error writing to byte buffer"
		text := scanner.Text()
		// Replace scalars from right to left so that each replacement leaves
		// the positions of the scalars before it unchanged.
		nodes := nodesByLine[line]
		slices.SortFunc(nodes, func(a, b *yaml.Node) int {
			return cmp.Compare(b.Column, a.Column)
		})
		for _, node := range nodes {
			text = replaceScalar(text, node, valuesByNode[node])
		}
		if _, err := outBuf.WriteString(text); err != nil {
			return nil, fmt.Errorf("%s: %w", errMsg, err)
		}
		if _, err := outBuf.WriteString("\n"); err != nil {
			return nil, fmt.Errorf("%s: %w", errMsg, err)
		}
		line++
	}
	if scanner.Err() != nil {
		return nil, fmt.Errorf("error scanning input bytes: %w", scanner.Err())
	}

	return outBuf.Bytes(), nil
}

// replaceScalar returns a copy of the provided line with the scalar that the
// provided node says begins on it replaced by the provided value. Anything
// following the scalar on the same line, such as a comment or the remaining
// entries of a flow collection, is preserved.
func replaceScalar(line string, node *yaml.Node, value any) string {
	// Node columns count characters, not bytes.
	start := byteOffset(line, node.Column-1)
	unchanged := line[:start]
	if !strings.HasSuffix(unchanged, " ") {
		unchanged += " "
	}
	length, ok := scalarLength(line[start:], node)
	if !ok {
		// The end of the scalar is not on this line, so replace everything from
		// the start of the scalar to the end of the line.
		return unchanged + fmt.Sprint(QuoteIfNecessary(value))
	}
	return unchanged +
		fmt.Sprint(formatValue(value, node.Style)) +
		line[start+length:]
}

// byteOffset returns the byte offset of the character at the provided index in
// the provided string, or the string's length if the index is out of range.
func byteOffset(s string, charIndex int) int {
	var i int
	for offset := range s {
		if i == charIndex {
			return offset
		}
		i++
	}
	return len(s)
}

// scalarLength returns the length in bytes of the scalar that the provided
// node describes and that the provided string begins with. It returns false if
// the end of the scalar cannot be found in the provided string, as with an
// empty scalar, a scalar that spans multiple lines, or a scalar preceded by an
// anchor or tag.
func scalarLength(s string, node *yaml.Node) (int, bool) {
	switch node.Style {
	case 0: // Plain
		// A plain scalar on a single line is written exactly as its value.
		if node.Value != "" && strings.HasPrefix(s, node.Value) {
			return len(node.Value), true
		}
	case yaml.DoubleQuotedStyle:
		return doubleQuotedLength(s)
	case yaml.SingleQuotedStyle:
		return singleQuotedLength(s)
	}
	return 0, false
}

// doubleQuotedLength returns the length in bytes, including quotes, of the
// double-quoted string that the provided string begins with. It returns false
// if the provided string does not begin with a closed double-quoted string.
func doubleQuotedLength(s string) (int, bool) {
	if !strings.HasPrefix(s, `"`) {
		return 0, false
	}
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++ // Skip the escaped character
		case '"':
			return i + 1, true
		}
	}
	return 0, false
}

// singleQuotedLength returns the length in bytes, including quotes, of the
// single-quoted string that the provided string begins with. It returns false
// if the provided string does not begin with a closed single-quoted string.
func singleQuotedLength(s string) (int, bool) {
	if !strings.HasPrefix(s, "'") {
		return 0, false
	}
	for i := 1; i < len(s); i++ {
		if s[i] != '\'' {
			continue
		}
		// Two consecutive single quotes are an escaped single quote.
		if i+1 < len(s) && s[i+1] == '\'' {
			i++
			continue
		}
		return i + 1, true
	}
	return 0, false
}

// formatValue returns the provided value formatted for writing in place of a
// scalar of the provided style. A string replacing a quoted string is quoted
// in the same way. Any other value is quoted only if necessary.
func formatValue(value any, style yaml.Style) any {
	if str, ok := value.(string); ok {
		switch style {
		case yaml.DoubleQuotedStyle:
			return strconv.Quote(str)
		case yaml.SingleQuotedStyle:
			return "'" + strings.ReplaceAll(str, "'", "''") + "'"
		}
	}
	return QuoteIfNecessary(value)
}

func findScalarNode(node *yaml.Node, keyPath []string) (*yaml.Node, error) {
	if len(keyPath) == 0 {
		if node.Kind == yaml.ScalarNode {
			return node, nil
		}
		return nil, fmt.Errorf("key path does not address a scalar node")
	}
	switch node.Kind {
	case yaml.DocumentNode:
		return findScalarNode(node.Content[0], keyPath)
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value == keyPath[0] {
				return findScalarNode(node.Content[i+1], keyPath[1:])
			}
		}
	case yaml.SequenceNode:
		index, err := strconv.Atoi(keyPath[0])
		if err != nil {
			return nil, err
		}
		return findScalarNode(node.Content[index], keyPath[1:])
	}
	return nil, fmt.Errorf("key path not found")
}
