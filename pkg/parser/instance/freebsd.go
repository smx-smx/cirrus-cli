package instance

import (
	"strconv"

	"github.com/cirruslabs/cirrus-cli/pkg/parser/instance/resources"
	"github.com/cirruslabs/cirrus-cli/pkg/parser/nameable"
	"github.com/cirruslabs/cirrus-cli/pkg/parser/node"
	"github.com/cirruslabs/cirrus-cli/pkg/parser/parseable"
	"github.com/cirruslabs/cirrus-cli/pkg/parser/parserkit"
	"github.com/cirruslabs/cirrus-cli/pkg/parser/schema"
	jsschema "github.com/lestrrat-go/jsschema"
)

// Environment variables through which a freebsd_instance definition is
// passed to the executor.
//
// There is deliberately no FreeBSD message in the API schema: on Cirrus
// Cloud freebsd_instance was resolved server-side into a GCE VM, so the
// CLI parser records the definition in the task environment instead and
// the executor boots the VM itself (under QEMU).
const (
	EnvFreeBSDImageFamily = "CIRRUS_FREEBSD_IMAGE_FAMILY"
	EnvFreeBSDImageName   = "CIRRUS_FREEBSD_IMAGE_NAME"
	EnvFreeBSDCPU         = "CIRRUS_FREEBSD_CPU"
	EnvFreeBSDMemory      = "CIRRUS_FREEBSD_MEMORY"
)

type FreeBSD struct {
	imageFamily string
	imageName   string
	cpu         float32
	memory      uint32

	parseable.DefaultParser
}

func NewFreeBSD(mergedEnv map[string]string, parserKit *parserkit.ParserKit) *FreeBSD {
	freebsd := &FreeBSD{}

	freebsd.OptionalField(nameable.NewSimpleNameable("image_family"), schema.String("FreeBSD image family to use (e.g. freebsd-14-4)."), func(node *node.Node) error {
		imageFamily, err := node.GetExpandedStringValue(mergedEnv)
		if err != nil {
			return err
		}
		freebsd.imageFamily = imageFamily
		return nil
	})

	freebsd.OptionalField(nameable.NewSimpleNameable("image_name"), schema.String("Concrete FreeBSD image to use (a full HTTPS URL to a .raw.xz image)."), func(node *node.Node) error {
		imageName, err := node.GetExpandedStringValue(mergedEnv)
		if err != nil {
			return err
		}
		freebsd.imageName = imageName
		return nil
	})

	freebsd.OptionalField(nameable.NewSimpleNameable("cpu"), schema.Number(""), func(node *node.Node) error {
		cpu, err := node.GetExpandedStringValue(mergedEnv)
		if err != nil {
			return err
		}
		cpuFloat, err := strconv.ParseFloat(cpu, 32)
		if err != nil {
			return err
		}
		freebsd.cpu = float32(cpuFloat)
		return nil
	})

	freebsd.OptionalField(nameable.NewSimpleNameable("memory"), schema.Memory(), func(node *node.Node) error {
		memory, err := node.GetExpandedStringValue(mergedEnv)
		if err != nil {
			return err
		}
		memoryParsed, err := resources.ParseMegaBytes(memory)
		if err != nil {
			return node.ParserError("%s", err.Error())
		}
		freebsd.memory = uint32(memoryParsed)
		return nil
	})

	return freebsd
}

func (freebsd *FreeBSD) Parse(
	node *node.Node,
	parserKit *parserkit.ParserKit,
) error {
	if err := freebsd.DefaultParser.Parse(node, parserKit); err != nil {
		return err
	}

	if freebsd.imageFamily == "" && freebsd.imageName == "" {
		return node.ParserError("freebsd_instance needs either \"image_family:\" or \"image_name:\" to be specified")
	}

	if freebsd.imageFamily != "" && freebsd.imageName != "" {
		return node.ParserError("please either use image_family: or image_name:, " +
			"since otherwise there's ambiguity about which image to prefer")
	}

	// Resource defaults
	if freebsd.cpu == 0 {
		freebsd.cpu = defaultCPU
	}
	if freebsd.memory == 0 {
		freebsd.memory = defaultMemory
	}

	return nil
}

	// Environment returns the parsed definition in the form the executor
	// understands (see the EnvFreeBSD* constants).
func (freebsd *FreeBSD) Environment() (map[string]string, error) {
	return map[string]string{
		EnvFreeBSDImageFamily: freebsd.imageFamily,
		EnvFreeBSDImageName:   freebsd.imageName,
		EnvFreeBSDCPU:         strconv.FormatFloat(float64(freebsd.cpu), 'f', -1, 32),
		EnvFreeBSDMemory:      strconv.FormatUint(uint64(freebsd.memory), 10),
	}, nil
}

func (freebsd *FreeBSD) Schema() *jsschema.Schema {	modifiedSchema := freebsd.DefaultParser.Schema()

	modifiedSchema.Type = jsschema.PrimitiveTypes{jsschema.ObjectType}
	modifiedSchema.Description = "FreeBSD Virtual Machine definition (runs under QEMU when executed via the CLI)."

	return modifiedSchema
}
