package yaml

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestSetValuesInBytes(t *testing.T) {
	testCases := []struct {
		name       string
		inBytes    []byte
		updates    []Update
		assertions func(*testing.T, []byte, error)
	}{
		{
			name: "invalid YAML",
			// Note: This YAML is invalid because one line is indented with a tab
			inBytes: []byte(`
characters:
- name: Anakin
	affiliation: Light side
`),
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.ErrorContains(t, err, "error unmarshaling input")
				require.Nil(t, bytes)
			},
		},
		{
			name: "no extra quotes around true number",
			inBytes: []byte(`
characters:
- name: Arthur Dent
  answer: idk
`),
			updates: []Update{
				{
					Key:   "characters.0.answer",
					Value: 42,
				},
			},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
characters:
- name: Arthur Dent
  answer: 42
`),
					bytes,
				)
			},
		},
		{
			name: "extra quotes around string containing a valid number",
			inBytes: []byte(`
characters:
- name: Arthur Dent
  answer: idk
`),
			updates: []Update{
				{
					Key:   "characters.0.answer",
					Value: "42",
				},
			},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
characters:
- name: Arthur Dent
  answer: "42"
`),
					bytes,
				)
			},
		},
		{
			name: "success",
			inBytes: []byte(`
characters:
- name: Anakin
  affiliation: Light side
`),
			updates: []Update{
				{
					Key:   "characters.0.affiliation",
					Value: "Dark side",
				},
			},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
characters:
- name: Anakin
  affiliation: Dark side
`),
					bytes,
				)
			},
		},
		{
			name: "non-ASCII characters before the value",
			inBytes: []byte(`
名前前: v1
`),
			updates: []Update{{Key: "名前前", Value: "v2"}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
名前前: v2
`),
					bytes,
				)
			},
		},
		{
			name: "comment after the value is preserved",
			inBytes: []byte(`
image:
  tag: v1.0.0 # pinned by ops
`),
			updates: []Update{{Key: "image.tag", Value: "v1.1.0"}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
image:
  tag: v1.1.0 # pinned by ops
`),
					bytes,
				)
			},
		},
		{
			name: "single-line flow mapping keeps its remaining entries",
			inBytes: []byte(`
image: {tag: v1, repo: foo}
`),
			updates: []Update{{Key: "image.tag", Value: "v2"}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
image: {tag: v2, repo: foo}
`),
					bytes,
				)
			},
		},
		{
			name: "flow sequence keeps its remaining entries",
			inBytes: []byte(`
tags: [v1, v2, v3]
`),
			updates: []Update{{Key: "tags.1", Value: "v4"}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
tags: [v1, v4, v3]
`),
					bytes,
				)
			},
		},
		{
			name: "all updates to values on the same line are applied",
			inBytes: []byte(`
image: {repo: foo, tag: v1}
`),
			updates: []Update{
				{
					Key:   "image.repo",
					Value: "ghcr.io/foo",
				},
				{
					Key:   "image.tag",
					Value: "v2",
				},
			},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
image: {repo: ghcr.io/foo, tag: v2}
`),
					bytes,
				)
			},
		},
		{
			name: "last of multiple updates to the same value wins",
			inBytes: []byte(`
image: {repo: foo, tag: v1}
`),
			updates: []Update{
				{
					Key:   "image.tag",
					Value: "v2",
				},
				{
					Key:   "image.tag",
					Value: "v3",
				},
			},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
image: {repo: foo, tag: v3}
`),
					bytes,
				)
			},
		},
		{
			name: "double quotes around a string value are preserved",
			inBytes: []byte(`
tag: "v1"
`),
			updates: []Update{{Key: "tag", Value: "v2"}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
tag: "v2"
`),
					bytes,
				)
			},
		},
		{
			name: "double quotes within a double-quoted value are escaped",
			inBytes: []byte(`
greeting: "hi"
`),
			updates: []Update{{Key: "greeting", Value: `say "bye"`}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
greeting: "say \"bye\""
`),
					bytes,
				)
			},
		},
		{
			name: "single quotes around a string value are preserved",
			inBytes: []byte(`
tag: 'v1'
`),
			updates: []Update{{Key: "tag", Value: "v2"}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
tag: 'v2'
`),
					bytes,
				)
			},
		},
		{
			name: "single quotes within a single-quoted value are escaped",
			inBytes: []byte(`
greeting: 'hi'
`),
			updates: []Update{{Key: "greeting", Value: "it's me"}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
greeting: 'it''s me'
`),
					bytes,
				)
			},
		},
		{
			name: "non-string value replacing a quoted value is not quoted",
			inBytes: []byte(`
replicas: "1" # scaled by ops
`),
			updates: []Update{{Key: "replicas", Value: 2}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
replicas: 2 # scaled by ops
`),
					bytes,
				)
			},
		},
		{
			name: "comma and hash inside a quoted value do not end it",
			inBytes: []byte(`
tag: "a, b # c" # note
`),
			updates: []Update{{Key: "tag", Value: "v2"}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
tag: "v2" # note
`),
					bytes,
				)
			},
		},
		{
			name: "escaped quote inside a double-quoted value does not end it",
			inBytes: []byte(`
greeting: "say \"hi\"" # note
`),
			updates: []Update{{Key: "greeting", Value: "bye"}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
greeting: "bye" # note
`),
					bytes,
				)
			},
		},
		{
			name: "escaped quote inside a single-quoted value does not end it",
			inBytes: []byte(`
greeting: 'it''s me' # note
`),
			updates: []Update{{Key: "greeting", Value: "bye"}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
greeting: 'bye' # note
`),
					bytes,
				)
			},
		},
		{
			// The node for an anchored scalar begins at the anchor rather than
			// at the scalar itself, so the end of the scalar cannot be found by
			// looking for its closing quote. The update must still produce
			// valid YAML with the new value.
			name: "anchored quoted value is replaced cleanly",
			inBytes: []byte(`
tag: &tag "v1"
`),
			updates: []Update{{Key: "tag", Value: "v2"}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				out := map[string]any{}
				require.NoError(t, yaml.Unmarshal(bytes, &out))
				require.Equal(t, "v2", out["tag"])
			},
		},
		{
			// The end of a scalar that spans multiple lines cannot be found on
			// the line where it begins. Updating it in place is not supported,
			// but it must not result in invalid YAML.
			name: "multi-line quoted value does not result in invalid YAML",
			inBytes: []byte(`
greeting: "hello
  world"
`),
			updates: []Update{{Key: "greeting", Value: "bye"}},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.NoError(t, yaml.Unmarshal(bytes, &map[string]any{}))
			},
		},
		{
			// This is the example from
			// https://github.com/akuity/kargo/issues/7839
			name: "KYAML keeps its commas and quotes",
			inBytes: []byte(`
version: {
  image: {
    tag: "0.1.0",
  },
  migrationBundleVersion: "0.1.0",
  gitCommit: "0000000",
}
`),
			updates: []Update{
				{
					Key:   "version.migrationBundleVersion",
					Value: "2026.10.16",
				},
				{
					Key:   "version.gitCommit",
					Value: "9adc75ba319c4ad134f443bfbbd3194e106c10c1",
				},
			},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					[]byte(`
version: {
  image: {
    tag: "0.1.0",
  },
  migrationBundleVersion: "2026.10.16",
  gitCommit: "9adc75ba319c4ad134f443bfbbd3194e106c10c1",
}
`),
					bytes,
				)
			},
		},
		{
			name: "really long lines still work",
			// nolint:lll
			inBytes: []byte(`
characters:
- name: Anakin
  affiliation: Light side
  temptation: "` + strings.Repeat("Did you ever hear the tragedy of Darth Plagueis The Wise? I thought not. It's not a story the Jedi would tell you. It's a Sith legend. Darth Plagueis was a Dark Lord of the Sith, so powerful and so wise he could use the Force to influence the midichlorians to create life...He had such a knowledge of the dark side that he could even keep the ones he cared about from dying. The dark side of the Force is a pathway to many abilities some consider to be unnatural. He became so powerful...the only thing he was afraid of was losing his power, which eventually, of course, he did. Unfortunately, he taught his apprentice everything he knew, then his apprentice killed him in his sleep. Ironic. He could save others from death, but not himself.", 1000) + `"
`),
			updates: []Update{
				{
					Key:   "characters.0.affiliation",
					Value: "Dark side",
				},
			},
			assertions: func(t *testing.T, bytes []byte, err error) {
				require.NoError(t, err)
				require.Equal(
					t,
					// nolint:lll
					[]byte(`
characters:
- name: Anakin
  affiliation: Dark side
  temptation: "`+strings.Repeat("Did you ever hear the tragedy of Darth Plagueis The Wise? I thought not. It's not a story the Jedi would tell you. It's a Sith legend. Darth Plagueis was a Dark Lord of the Sith, so powerful and so wise he could use the Force to influence the midichlorians to create life...He had such a knowledge of the dark side that he could even keep the ones he cared about from dying. The dark side of the Force is a pathway to many abilities some consider to be unnatural. He became so powerful...the only thing he was afraid of was losing his power, which eventually, of course, he did. Unfortunately, he taught his apprentice everything he knew, then his apprentice killed him in his sleep. Ironic. He could save others from death, but not himself.", 1000)+`"
`),
					bytes,
				)
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			b, err := SetValuesInBytes(testCase.inBytes, testCase.updates)
			testCase.assertions(t, b, err)
		})
	}
}

func TestFindScalarNode(t *testing.T) {
	yamlBytes := []byte(`
characters:
  rebels:
  - name: Skywalker
`)
	testCases := []struct {
		name       string
		keyPath    string
		assertions func(t *testing.T, node *yaml.Node, err error)
	}{
		{
			name:    "node not found",
			keyPath: "characters.imperials",
			assertions: func(t *testing.T, _ *yaml.Node, err error) {
				require.ErrorContains(t, err, "key path not found")
			},
		},
		{
			name: "node not found due to error parsing int",
			// Really, this is a special case of a key that doesn't address a node,
			// because there is alpha input where numeric input would be expected.
			keyPath: "characters.rebels.first.name",
			assertions: func(t *testing.T, _ *yaml.Node, err error) {
				require.ErrorContains(t, err, "strconv.Atoi")
			},
		},
		{
			name:    "node found, but isn't a scalar node",
			keyPath: "characters.rebels",
			assertions: func(t *testing.T, _ *yaml.Node, err error) {
				require.ErrorContains(t, err, "key path does not address a scalar node")
			},
		},
		{
			name:    "success",
			keyPath: "characters.rebels.0.name",
			assertions: func(t *testing.T, node *yaml.Node, err error) {
				require.NoError(t, err)
				require.Equal(t, "Skywalker", node.Value)
				require.Equal(t, 4, node.Line)
				require.Equal(t, 11, node.Column)
			},
		},
	}
	doc := &yaml.Node{}
	err := yaml.Unmarshal(yamlBytes, doc)
	require.NoError(t, err)
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			node, err := findScalarNode(doc, strings.Split(testCase.keyPath, "."))
			testCase.assertions(t, node, err)
		})
	}
}
