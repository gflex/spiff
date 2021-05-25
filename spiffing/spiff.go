// package spiffing is a wrapper for internal spiff functionality
// distributed over multiple packaes to offer a coherent interface
// for using spiff as go library

package spiffing

import (
	"github.com/mandelsoft/vfs/pkg/osfs"
	"github.com/mandelsoft/vfs/pkg/vfs"

	"github.com/mandelsoft/spiff/dynaml"
	"github.com/mandelsoft/spiff/features"
	"github.com/mandelsoft/spiff/flow"
	"github.com/mandelsoft/spiff/yaml"
)

type sourceBase struct {
	name string
}

func (s *sourceBase) Name() string {
	return s.name
}

type sourceFile struct {
	sourceBase
	fs vfs.FileSystem
}

// NewSourceFile returns a source based on a file in a virtual filesystem
// If no filesystem is given the os filesystem is used by default
func NewSourceFile(path string, optfs ...vfs.FileSystem) Source {
	var fs vfs.FileSystem
	if len(optfs) > 0 {
		fs = optfs[0]
	}
	if fs == nil {
		fs = osfs.New()
	}
	return &sourceFile{sourceBase{path}, fs}
}

func (s *sourceFile) Data() ([]byte, error) {
	return vfs.ReadFile(s.fs, s.name)
}

type sourceData struct {
	sourceBase
	data []byte
}

// NewSourceData creates a source based on yaml data
func NewSourceData(name string, data []byte) Source {
	return &sourceData{sourceBase{name}, data}
}

func (s *sourceData) Data() ([]byte, error) {
	return s.data, nil
}

////////////////////////////////////////////////////////////////////////////////

type spiff struct {
	key           string
	mode          int
	fs            vfs.FileSystem
	opts          flow.Options
	values        map[string]yaml.Node
	functions     Functions
	tags          map[string]*dynaml.Tag
	interpolation bool

	binding dynaml.Binding
}

// NewFunctions provides a new registry for additional spiff functions
func NewFunctions() Functions {
	return dynaml.NewRegistry()
}

// New create a new default spiff context.
func New() Spiff {
	set := features.Features()
	_, interpolation := set[features.INTERPOLATION]
	return &spiff{
		key:           features.EncryptionKey(),
		mode:          MODE_DEFAULT,
		interpolation: interpolation,
	}
}

func (s *spiff) Reset() Spiff {
	s.binding = nil
	return s
}

func (s *spiff) ResetStream() Spiff {
	flow.ResetStream(s.binding)
	return s
}

func (s *spiff) assureBinding() {
	if s.binding == nil {
		state := flow.NewState(s.key, s.mode, s.fs).
			SetFunctions(s.functions).
			SetInterpolation(s.interpolation)
		if len(s.tags) > 0 {
			var tags []*dynaml.Tag
			for _, t := range s.tags {
				tags = append(tags, t)
			}
			state.SetTags(tags...)
		}
		s.binding = flow.NewEnvironment(
			nil, "context", state)
		if s.values != nil {
			s.binding = s.binding.WithLocalScope(s.values)
		}
	}
}

// WithInterpolation creates a new context with
// enabled/disabled string interpolation feature
func (s spiff) WithInterpolation(b bool) Spiff {
	s.interpolation = b
	return s.Reset()
}

// WithEncryptionKey creates a new context with
// dedicated encryption key used for the spiff encryption feature
func (s spiff) WithEncryptionKey(key string) Spiff {
	s.key = key
	return s.Reset()
}

// WithMode creates a new context with the given processing mode.
// (see MODE constants)
func (s spiff) WithMode(mode int) Spiff {
	if s.fs != nil {
		mode = mode & ^MODE_OS_ACCESS
	}
	s.mode = mode
	return s.Reset()
}

// WithFileSystem creates a new context with the given
// virtual filesystem used for filesystem functions during
// prcessing. Setting a filesystem disables the command
// execution functions.
func (s spiff) WithFileSystem(fs vfs.FileSystem) Spiff {
	s.fs = fs
	if fs != nil {
		s.mode = s.mode & ^MODE_OS_ACCESS
	}
	return s.Reset()
}

// WithFunctions creates a new context with the given
// additional function definitions
func (s spiff) WithFunctions(functions Functions) Spiff {
	s.functions = functions
	return s.Reset()
}

// WithValues creates a new context with the given
// additional structured values usable by path expressions
// during processing.
// It is highly recommended to decide for a common root
// value (like `values`) to minimize the blocked root
// elements in the processed documents.
func (s spiff) WithValues(values map[string]interface{}) (Spiff, error) {
	if values != nil {
		nodes, err := yaml.Sanitize("values", values)
		if err != nil {
			return nil, err
		}
		s.values = nodes.Value().(map[string]yaml.Node)
	} else {
		s.values = nil
	}
	return s.Reset(), nil
}

// SetTag sets/resets a global tag for subsequent processings.
func (s spiff) SetTag(tag string, node yaml.Node) Spiff {
	s.tags[tag] = dynaml.NewTag(tag, node, nil, true)
	return s.Reset()
}

// FileSystem return the virtual filesystem set for the execution context.
func (s *spiff) FileSystem() vfs.FileSystem {
	return s.fs
}

// FileSource create a new file source based on the configured file system.
func (s *spiff) FileSource(path string) Source {
	return NewSourceFile(path, s.fs)
}

// CleanupTags deletes all gathered tags
func (s *spiff) CleanupTags() Spiff {
	s.binding = nil
	s.tags = map[string]*dynaml.Tag{}
	return s
}

// Cascade processes a template with a list of given subs and state
// documents
func (s *spiff) Cascade(template Node, stubs []Node, states ...Node) (Node, error) {
	s.Reset()
	s.assureBinding()
	defer s.Reset()
	return flow.Cascade(s.binding, template, s.opts, append(stubs, states...)...)
}

// PrepareStubs processes a list a stubs and returns a prepared
// representation usable to process a template
// Global tags provided by the stubs are kept until the next
// Prepare or Cascade processing.
func (s *spiff) PrepareStubs(stubs ...Node) ([]Node, error) {
	s.Reset()
	s.assureBinding()
	return flow.PrepareStubs(s.binding, s.opts.Partial, stubs...)
}

// ApplyStubs uses already prepared subs to process a template.
// It uses the configured implicit tag settings.
func (s *spiff) ApplyStubs(template Node, preparedstubs []Node, stream ...bool) (Node, error) {
	s.assureBinding()
	if len(stream) == 0 || !stream[0] {
		s.ResetStream()
	}
	return flow.Apply(s.binding, template, preparedstubs, s.opts)
}

// Unmarshal parses a single document yaml representation and
// returns the internal representation
func (s *spiff) Unmarshal(name string, source []byte) (Node, error) {
	return yaml.Unmarshal(name, source)
}

// Unmarshal parses a single source and
// returns the internal representation
func (s *spiff) UnmarshalSource(source Source) (Node, error) {
	data, err := source.Data()
	if err != nil {
		return nil, err
	}
	return yaml.Unmarshal(source.Name(), data)
}

// UnmarshalMulti parses a multi document yaml representation and
// returns the list of documents in the internal representation
func (s *spiff) UnmarshalMulti(name string, source []byte) ([]Node, error) {
	return yaml.UnmarshalMulti(name, source)
}

// UnmarshalMulti parses a multi document source and
// returns the list of documents in the internal representation
func (s *spiff) UnmarshalMultiSource(source Source) ([]Node, error) {
	data, err := source.Data()
	if err != nil {
		return nil, err
	}
	return yaml.UnmarshalMulti(source.Name(), data)
}

// DetermineState extracts the intended new state representation from
// a processing result.
func (s *spiff) DetermineState(node Node) Node {
	return flow.DetermineState(node)
}

// Marshal transform the internal node representation into a
// yaml representation
func (s *spiff) Marshal(node Node) ([]byte, error) {
	return yaml.Marshal(node)
}

// Normalize transform the node representation to a regular go value representation
// consisting of map[string]interface{}`, `[]interface{}`, `string `boolean`,
// `int64`, `float64` and []byte objects
func (s *spiff) Normalize(node Node) (interface{}, error) {
	return yaml.Normalize(node)
}
