package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/deislabs/oras/pkg/content"
	"github.com/deislabs/oras/pkg/oras"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	annotationConfig   = "$config"
	annotationManifest = "$manifest"
)

type pushOptions struct {
	targetRef           string
	fileRefs            []string
	manifestConfigRef   string
	manifestAnnotations string

	debug    bool
	configs  []string
	username string
	password string
}

func pushCmd() *cobra.Command {
	var opts pushOptions
	cmd := &cobra.Command{
		Use:   "push name[:tag|@digest] file[:type] [file...]",
		Short: "Push files to remote registry",
		Long: `Push files to remote registry

Example - Push file "hi.txt" with the "application/vnd.oci.image.layer.v1.tar" media type (default):
  oras push localhost:5000/hello:latest hi.txt

Example - Push file "hi.txt" with the custom "application/vnd.me.hi" media type:
  oras push localhost:5000/hello:latest hi.txt:application/vnd.me.hi

Example - Push multiple files with different media types:
  oras push localhost:5000/hello:latest hi.txt:application/vnd.me.hi bye.txt:application/vnd.me.bye

Example - Push file "hi.txt" with the custom manifest config "config.json" of the custom "application/vnd.me.config" media type:
  oras push --manifest-config config.json:application/vnd.me.config localhost:5000/hello:latest hi.txt
`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.targetRef = args[0]
			opts.fileRefs = args[1:]
			return runPush(opts)
		},
	}

	cmd.Flags().StringVarP(&opts.manifestConfigRef, "manifest-config", "", "", "manifest config file")
	cmd.Flags().StringVarP(&opts.manifestAnnotations, "manifest-annotations", "", "", "manifest annotation file")
	cmd.Flags().BoolVarP(&opts.debug, "debug", "d", false, "debug mode")
	cmd.Flags().StringArrayVarP(&opts.configs, "config", "c", nil, "auth config path")
	cmd.Flags().StringVarP(&opts.username, "username", "u", "", "registry username")
	cmd.Flags().StringVarP(&opts.password, "password", "p", "", "registry password")
	return cmd
}

func runPush(opts pushOptions) error {
	if opts.debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	// load files
	var (
		annotations map[string]map[string]string
		files       []ocispec.Descriptor
		store       = content.NewFileStore("")
		pushOpts    []oras.PushOpt
	)
	if opts.manifestAnnotations != "" {
		if err := decodeJSON(opts.manifestAnnotations, &annotations); err != nil {
			return err
		}
		if value, ok := annotations[annotationConfig]; ok {
			pushOpts = append(pushOpts, oras.WithConfigAnnotations(value))
		}
		if value, ok := annotations[annotationManifest]; ok {
			pushOpts = append(pushOpts, oras.WithManifestAnnotations(value))
		}
	}
	if opts.manifestConfigRef != "" {
		ref := strings.SplitN(opts.manifestConfigRef, ":", 2)
		filename := ref[0]
		mediaType := ocispec.MediaTypeImageConfig
		if len(ref) == 2 {
			mediaType = ref[1]
		}
		file, err := store.Add(annotationConfig, mediaType, filename)
		if err != nil {
			return err
		}
		file.Annotations = nil
		pushOpts = append(pushOpts, oras.WithConfig(file))
	}
	for _, fileRef := range opts.fileRefs {
		ref := strings.SplitN(fileRef, ":", 2)
		filename := ref[0]
		var mediaType string
		if len(ref) == 2 {
			mediaType = ref[1]
		}
		file, err := store.Add(filename, mediaType, "")
		if err != nil {
			return err
		}
		if annotations != nil {
			if value, ok := annotations[filename]; ok {
				if file.Annotations == nil {
					file.Annotations = value
				} else {
					for k, v := range value {
						file.Annotations[k] = v
					}
				}
			}
		}
		files = append(files, file)
	}

	// ready to push
	resolver := newResolver(opts.username, opts.password, opts.configs...)
	return oras.Push(context.Background(), resolver, opts.targetRef, store, files, pushOpts...)
}

func decodeJSON(filename string, v interface{}) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewDecoder(file).Decode(v)
}
