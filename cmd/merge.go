package cmd

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mandelsoft/spiff/debug"
	"github.com/mandelsoft/spiff/dynaml"
	"github.com/mandelsoft/spiff/features"
	"github.com/mandelsoft/spiff/flow"
	"github.com/mandelsoft/spiff/legacy/candiedyaml"
	"github.com/mandelsoft/spiff/yaml"
)

var asJSON bool
var outputPath string
var selection []string
var tagdefs []string
var expr string
var split bool
var interpolation bool
var featureFlags []string
var processingOptions flow.Options
var state string
var bindings string
var values []string

// mergeCmd represents the merge command
var mergeCmd = &cobra.Command{
	Use:     "merge",
	Aliases: []string{"m"},
	Short:   "Merge stub files into a manifest template",
	Long:    `Merge a bunch of template files into one manifest, printing it out.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return errors.New("requires at least one arg")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		vals, err := createValuesFromArgs(values)
		if err != nil {
			log.Fatalf("%s\n", err)
		}
		merge(false, args[0], processingOptions, asJSON, split, outputPath, selection, state, bindings, vals, nil, args[1:])
	},
}

func init() {

	set := features.Features()
	_, interpolation = set[features.INTERPOLATION]
	rootCmd.AddCommand(mergeCmd)

	mergeCmd.Flags().BoolVar(&interpolation, "interpolation", interpolation, "enable interpolation alpha feature")
	mergeCmd.Flags().BoolVar(&asJSON, "json", false, "print output in json format")
	mergeCmd.Flags().BoolVar(&debug.DebugFlag, "debug", false, "Print state info")
	mergeCmd.Flags().BoolVar(&processingOptions.Partial, "partial", false, "Allow partial evaluation only")
	mergeCmd.Flags().StringVar(&outputPath, "path", "", "output is taken from given path")
	mergeCmd.Flags().BoolVar(&split, "split", false, "if the output is a list it will be split into separate documents")
	mergeCmd.Flags().BoolVar(&processingOptions.PreserveEscapes, "preserve-escapes", false, "preserve escaping for escaped expressions and merges")
	mergeCmd.Flags().BoolVar(&processingOptions.PreserveTemporary, "preserve-temporary", false, "preserve temporary fields")
	mergeCmd.Flags().StringVar(&state, "state", "", "select state file to maintain")
	mergeCmd.Flags().StringVar(&bindings, "bindings", "", "yaml file with additional bindings to use")
	mergeCmd.Flags().StringArrayVarP(&values, "define", "D", nil, "key/value bindings")
	mergeCmd.Flags().StringArrayVar(&selection, "select", []string{}, "filter dedicated output fields")
	mergeCmd.Flags().StringArrayVar(&tagdefs, "tag", []string{}, "tag files (tag:path)")
	mergeCmd.Flags().StringArrayVar(&featureFlags, "features", []string{}, "set feature flags")
	mergeCmd.Flags().StringVar(&expr, "evaluate", "", "evaluation expression")
}

func createValuesFromArgs(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := map[string]string{}
	for _, s := range values {
		parts := strings.Split(s, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid value definition %q\n", s)
		}
		if parts[0] == "" {
			return nil, fmt.Errorf("empty key in value definition %q\n", s)
		}
		result[parts[0]] = parts[1]
	}
	return result, nil
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func readYAML(filename string, desc string, required bool) yaml.Node {
	if filename != "" {
		if fileExists(filename) {
			data, err := ioutil.ReadFile(filename)
			if required && err != nil {
				log.Fatalln(fmt.Sprintf("error reading %s [%s]:", desc, path.Clean(filename)), err)
			}
			doc, err := yaml.Parse(filename, data)
			if err != nil {
				log.Fatalln(fmt.Sprintf("error parsing %s [%s]:", desc, path.Clean(filename)), err)
			}
			return doc
		}
	}
	return nil
}

func merge(stdin bool, templateFilePath string, opts flow.Options, json, split bool,
	subpath string, selection []string, stateFilePath, bindingFilePath string, values map[string]string, stubs []yaml.Node, stubFilePaths []string) {
	var templateFile []byte
	var err error

	if templateFilePath == "-" {
		templateFile, err = ioutil.ReadAll(os.Stdin)
		stdin = true
	} else {
		templateFile, err = ReadFile(templateFilePath)
	}

	if err != nil {
		log.Fatalln(fmt.Sprintf("error reading template [%s]:", path.Clean(templateFilePath)), err)
	}

	templateYAMLs, err := yaml.ParseMulti(templateFilePath, templateFile)
	if err != nil {
		log.Fatalln(fmt.Sprintf("error parsing template [%s]:", path.Clean(templateFilePath)), err)
	}

	var stateYAML yaml.Node
	if stateFilePath != "" {
		if len(templateYAMLs) > 1 {
			log.Fatalln(fmt.Sprintf("state handling not supported gor multi documents [%s]:", path.Clean(templateFilePath)), err)
		}
		stateYAML = readYAML(stateFilePath, "state file", false)
	}
	bindingYAML := readYAML(bindingFilePath, "bindings file", true)

	if len(values) > 0 {
		if bindingYAML == nil {
			bindingYAML = yaml.NewNode(map[string]yaml.Node{}, "<values>")
		}
		m, ok := bindingYAML.Value().(map[string]yaml.Node)
		if !ok {
			log.Fatalf(fmt.Sprintf("binding %q must be a map\n", bindingFilePath))
		}
		for k, v := range values {
			i, err := strconv.ParseInt(v, 10, 64)
			if err == nil {
				err = addValue(m, k, yaml.NewNode(i, "<values>"))
			} else {
				err = addValue(m, k, yaml.NewNode(v, "<values>"))
			}
			if err != nil {
				log.Fatalln(fmt.Sprintf("error in value definitions (-D): %s", err))
			}
		}

	}

	tags := []*dynaml.Tag{}

	for _, tagDef := range tagdefs {
		i := strings.Index(tagDef, ":")
		if i <= 0 {
			log.Fatalln(fmt.Sprintf("tag file must be preceeded by a tag (<tag>:<path>)"))
		}
		tagName := tagDef[:i]
		err := dynaml.CheckTagName(tagName)
		if err != nil {
			log.Fatalln(fmt.Sprintf("invalid tag name [%s]:", path.Clean(tagName)), err)
		}
		tagFilePath := tagDef[i+1:]
		tagFile, err := ReadFile(tagFilePath)
		if err != nil {
			log.Fatalln(fmt.Sprintf("error reading tag file [%s]:", path.Clean(tagFilePath)), err)
		}

		tagYAML, err := yaml.Parse(tagFilePath, tagFile)
		if err != nil {
			log.Fatalln(fmt.Sprintf("error parsing tag file [%s]:", path.Clean(tagFilePath)), err)
		}

		tags = append(tags, dynaml.NewTag(tagName, tagYAML, nil, dynaml.TAG_SCOPE_GLOBAL))
	}

	if stubs == nil {
		stubs = []yaml.Node{}
	}
	stubs = append(stubs[:0:0], stubs...)

	for _, stubFilePath := range stubFilePaths {
		var stubFile []byte
		var err error
		if stubFilePath == "-" {
			if stdin {
				log.Fatalln(fmt.Sprintf("stdin cannot be used twice"))
			}
			stubFile, err = ioutil.ReadAll(os.Stdin)
			stdin = true
		} else {
			stubFile, err = ReadFile(stubFilePath)
		}
		if err != nil {
			log.Fatalln(fmt.Sprintf("error reading stub [%s]:", path.Clean(stubFilePath)), err)
		}

		stubYAML, err := yaml.Parse(stubFilePath, stubFile)
		if err != nil {
			log.Fatalln(fmt.Sprintf("error parsing stub [%s]:", path.Clean(stubFilePath)), err)
		}

		stubs = append(stubs, stubYAML)
	}

	if stateYAML != nil {
		stubs = append(stubs, stateYAML)
	}

	legend := "\nerror classification:\n" +
		" *: error in local dynaml expression\n" +
		" @: dependent of or involved in a cycle\n" +
		" -: depending on a node with an error"

	var binding dynaml.Binding
	features := features.Features()
	for _, list := range featureFlags {
		for _, f := range strings.Split(list, ",") {
			if err := features.Set(strings.TrimSpace(f), true); err != nil {
				log.Fatalln(err.Error())
			}
		}
	}
	if interpolation {
		features.SetInterpolation(true)
	}
	if bindingYAML != nil || features.Size() > 0 || len(tags) > 0 || len(templateYAMLs) > 1 {
		defstate := flow.NewDefaultState().SetTags(tags...).SetFeatures(features)
		binding = flow.NewEnvironment(
			nil, "context", defstate)
		if bindingYAML != nil {
			values, ok := bindingYAML.Value().(map[string]yaml.Node)
			if !ok {
				log.Fatalln("bindings must be given as map")
			}
			binding = binding.WithLocalScope(values)
		}
		features = binding.GetFeatures()
	}

	prepared, err := flow.PrepareStubs(binding, processingOptions.Partial, stubs...)
	if !processingOptions.Partial && err != nil {
		log.Fatalln("error generating manifest:", err, legend)
	}

	result := [][]byte{}
	count := 0
	for no, templateYAML := range templateYAMLs {
		doc := ""
		if len(templateYAMLs) > 1 {
			doc = fmt.Sprintf(" (document %d)", no+1)
		}
		var bytes []byte
		if templateYAML.Value() != nil {
			count++
			flowed, err := flow.Apply(binding, templateYAML, prepared, opts)
			if !opts.Partial && err != nil {
				log.Fatalln(fmt.Sprintf("error generating manifest%s:", doc), err, legend)
			}
			if err != nil {
				flowed = dynaml.ResetUnresolvedNodes(flowed)
			}
			if !opts.PreserveTemporary && flowed.Temporary() {
				continue
			}
			if subpath != "" {
				comps := dynaml.PathComponents(subpath, false)
				node, ok := yaml.FindR(true, flowed, features, comps...)
				if !ok {
					log.Fatalln(fmt.Sprintf("path %q not found%s", subpath, doc))
				}
				flowed = node
			}
			if stateFilePath != "" {
				state := flow.Cleanup(flowed, flow.DiscardNonState)
				json := json
				if strings.HasSuffix(stateFilePath, ".yaml") || strings.HasSuffix(stateFilePath, ".yml") {
					json = false
				} else {
					if strings.HasSuffix(stateFilePath, ".json") {
						json = true
					}
				}
				if json {
					bytes, err = yaml.ToJSON(state)
				} else {
					bytes, err = candiedyaml.Marshal(state)
				}
				old := false
				if fileExists(stateFilePath) {
					os.Rename(stateFilePath, stateFilePath+".bak")
					old = true
				}
				err := ioutil.WriteFile(stateFilePath, bytes, 0664)
				if err != nil {
					os.Remove(stateFilePath)
					os.Remove(stateFilePath)
					if old {
						os.Rename(stateFilePath+".bak", stateFilePath)
					}
					log.Fatalln(fmt.Sprintf("cannot write state file %q", stateFilePath))
				}
			}

			if len(expr) > 0 {
				e, err := dynaml.Parse(expr, []string{}, []string{})
				if err != nil {
					log.Fatalln(fmt.Sprintf("invalid expression %q: %s", expr, err))
				}
				if m, ok := flowed.Value().(map[string]yaml.Node); ok {
					binding := flow.NewNestedEnvironment(nil, "context", binding).WithLocalScope(m)
					v, err := flow.Cascade(binding, yaml.NewNode(e, "<expr>"), flow.Options{})
					if err != nil {
						log.Fatalln(fmt.Sprintf("expression %q failed: %s", expr, err))
					}
					flowed = v
				} else {
					log.Fatalln("no map document")
				}
			}

			if len(selection) > 0 {
				new := map[string]yaml.Node{}
				for _, p := range selection {
					comps := dynaml.PathComponents(p, false)
					node, ok := yaml.FindR(true, flowed, features, comps...)
					if !ok {
						log.Fatalln(fmt.Sprintf("path %q not found%s", subpath, doc))
					}
					new[comps[len(comps)-1]] = node

				}
				flowed = yaml.NewNode(new, "")
			}

			if split {
				if list, ok := flowed.Value().([]yaml.Node); ok {
					for _, d := range list {
						if json {
							bytes, err = yaml.ToJSON(d)
						} else {
							bytes, err = candiedyaml.Marshal(d)
						}
						if err != nil {
							log.Fatalln(fmt.Sprintf("error marshalling manifest%s:", doc), err)
						}
						result = append(result, bytes)
					}
					continue
				}
			}
			if json {
				bytes, err = yaml.ToJSON(flowed)
			} else {
				bytes, err = candiedyaml.Marshal(flowed)
			}
			if err != nil {
				log.Fatalln(fmt.Sprintf("error marshalling manifest%s:", doc), err)
			}
		}
		result = append(result, bytes)
	}

	for _, bytes := range result {
		if !json && (len(result) > 1 || len(bytes) == 0) {
			fmt.Println("---")
		}
		if bytes != nil {
			fmt.Print(string(bytes))
			if json {
				fmt.Println()
			}
		}
	}
}

func addValue(m map[string]yaml.Node, name string, value yaml.Node) error {
	comps := strings.Split(name, ".")
	for i := 0; i < len(comps)-1; i++ {
		if comps[i] == "" {
			return fmt.Errorf("empty path component in %q", name)
		}
		mask := 0
		for strings.HasSuffix(comps[i], "\\") {
			mask++
			comps[i] = comps[i][:len(comps[i])-1]
		}
		for mask > 1 {
			comps[i] += "\\"
			mask -= 2
		}
		if mask > 0 {
			comps[i] += "." + comps[i+1]
			copy(comps[i+1:], comps[i+2:])
			comps = comps[:len(comps)-1]
			i--
		}
	}
	for i := 0; i < len(comps)-1; i++ {
		c := comps[i]
		if m[c] == nil {
			n := map[string]yaml.Node{}
			m[c] = yaml.NewNode(n, "<values>")
			m = n
		} else {
			if n, ok := m[c].Value().(map[string]yaml.Node); !ok {
				return fmt.Errorf("field %q in %s is no map", c, name)
			} else {
				m = n
			}
		}
	}
	m[comps[len(comps)-1]] = value
	return nil
}
