// Package videoreq shapes a CLI request document into the payload the platform
// actually forwards to a provider.
//
// POST /integration/video-tasks stores items[].inputImages for detail display,
// moderation and subject recognition only; each provider model reads its own
// dedicated field instead. The web client derives those fields from its single
// material list (zriyo-web/app/api/workspace/video-task.ts), and the CLI used to
// post the document verbatim, so a caller that filled only inputImages got a
// single image at the provider. This package closes that gap so one request
// document behaves the same on both paths:
//
//   - Seedance: inputImages[0] -> items[].inputImageUrl,
//     inputImages[1..] -> items[].seedanceConfig.referenceImages
//     (KlingOmniVideoModeAdapter is not involved;
//     see the Seedance adapter's provider payload).
//   - kling-v3-omni: inputImages[0] -> items[].inputImageUrl,
//     inputImages[1..] -> items[].omniConfig.imageList
//     (KlingOmniVideoModeAdapter.addImageList builds the provider image_list as
//     inputImageUrl plus omniConfig.imageList).
//   - Every other model takes a single image, so web and CLI already agree and
//     nothing is rewritten.
package videoreq

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// seedanceModelCodes mirrors isSeedanceModelCode in the web client
// (app/api/workspace/video-task.ts). SEEDANCE is the web-only alias the client
// rewrites to SEEDANCE_2_0; it is accepted here so the same document works.
var seedanceModelCodes = map[string]bool{
	"SEEDANCE":          true,
	"SEEDANCE_2_0":      true,
	"SEEDANCE_2_5":      true,
	"SEEDANCE_2_0_FAST": true,
	"SEEDANCE_2_0_MINI": true,
}

// omniModelCode is the kling omni model code. The provider adapter matches it
// case-insensitively (KlingOmniVideoModeAdapter.supports), so this does too.
const omniModelCode = "kling-v3-omni"

// Provider fields a rewritten list lands in, reported back to the caller.
const (
	seedanceReferenceField = "seedanceConfig.referenceImages"
	omniReferenceField     = "omniConfig.imageList"
)

// Key names of an existing provider list, used to tell an already-shaped list
// from an absent one. The Seedance DTO entry uses oosKey/url, the omni image
// reference uses imageOosKey/imageUrl (VideoOmniConfigDTO.ImageReference).
var (
	seedanceReferenceKeys = [2]string{"oosKey", "url"}
	omniImageKeys         = [2]string{"imageOosKey", "imageUrl"}
)

// Report describes what normalization rewrote, so the caller can disclose it.
type Report struct {
	// ItemsWithReferenceImages counts items that received a reference list.
	ItemsWithReferenceImages int
	// ReferenceImages is the total number of reference images injected.
	ReferenceImages int
	// PrimaryImagesFilled counts items whose inputImageUrl / inputImageOosKey
	// were filled from the first usable inputImages entry.
	PrimaryImagesFilled int
	// ReferenceField is the provider field the injected references were written
	// to (seedanceConfig.referenceImages or omniConfig.imageList), so the
	// disclosure names the field that actually reaches the provider.
	ReferenceField string
}

// Changed reports whether normalization rewrote anything.
func (r Report) Changed() bool {
	return r.ItemsWithReferenceImages > 0 || r.PrimaryImagesFilled > 0
}

// material is one usable inputImages entry: it carries a URL or an OOS key.
type material struct {
	OosKey string
	URL    string
	Meta   any
}

// NormalizeInputImages applies the web client's request shaping to a video
// create document: the caller fills one material list (items[].inputImages) and
// the CLI derives the provider fields from it, exactly as the web client does
// before POST /integration/video-tasks. Models that take a single image need no
// rewrite and are returned untouched. The returned bytes are byte-identical to
// raw when nothing needed rewriting, so callers can safely use the output as the
// digest and transport payload.
func NormalizeInputImages(raw []byte) ([]byte, Report, error) {
	doc, err := decodeObject(raw)
	if err != nil {
		return nil, Report{}, err
	}
	code := strings.ToUpper(strings.TrimSpace(stringField(doc, "modelCode")))

	var (
		report Report
		write  referenceWriter
	)
	switch {
	case isSeedanceModel(code):
		report.ReferenceField = seedanceReferenceField
		write = fillReferenceImages
	case isOmniModel(code):
		report.ReferenceField = omniReferenceField
		write = fillOmniImageList
	default:
		return raw, Report{}, nil
	}
	if err := shapeItems(doc, &report, write); err != nil {
		return nil, Report{}, err
	}
	if !report.Changed() {
		return raw, Report{}, nil
	}
	normalized, err := encodeObject(doc)
	if err != nil {
		return nil, Report{}, err
	}
	return normalized, report, nil
}

// referenceWriter stores the extra materials in the provider field that the
// selected model reads.
type referenceWriter func(item map[string]any, extras []material, index int, report *Report) error

// shapeItems walks every item, fills the primary image and hands the remaining
// materials to the model-specific writer. Items without a usable material are
// left alone.
func shapeItems(doc map[string]any, report *Report, write referenceWriter) error {
	items, ok := doc["items"].([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	for index, entry := range items {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		materials := materialList(item["inputImages"])
		if len(materials) == 0 {
			continue
		}
		fillPrimaryImage(item, materials[0], report)
		if err := write(item, materials[1:], index, report); err != nil {
			return err
		}
	}
	return nil
}

// fillPrimaryImage mirrors inputImageUrl: inputImages[0] on the web path. An
// explicitly supplied primary image wins: only a blank pair is filled, so a
// caller that intentionally points inputImageUrl at a different frame keeps it.
func fillPrimaryImage(item map[string]any, first material, report *Report) {
	if stringField(item, "inputImageUrl") != "" || stringField(item, "inputImageOosKey") != "" {
		return
	}
	if first.URL != "" {
		item["inputImageUrl"] = first.URL
	}
	if first.OosKey != "" {
		item["inputImageOosKey"] = first.OosKey
	}
	if _, exists := item["inputImageMetadata"]; !exists && first.Meta != nil {
		item["inputImageMetadata"] = first.Meta
	}
	report.PrimaryImagesFilled++
}

// fillReferenceImages mirrors referenceImages: inputImages.slice(1) on the web
// path. An existing referenceImages list is never appended to or replaced, so
// the mapping cannot fire twice on a request that is replayed through the CLI.
func fillReferenceImages(item map[string]any, extras []material, index int, report *Report) error {
	if len(extras) == 0 {
		return nil
	}
	config, err := seedanceConfig(item, index)
	if err != nil {
		return err
	}
	if len(usableEntries(config["referenceImages"], seedanceReferenceKeys)) > 0 {
		return nil
	}
	references := make([]any, 0, len(extras))
	for _, extra := range extras {
		entry := map[string]any{}
		if extra.OosKey != "" {
			entry["oosKey"] = extra.OosKey
		}
		if extra.URL != "" {
			entry["url"] = extra.URL
		}
		if extra.Meta != nil {
			entry["metadata"] = extra.Meta
		}
		references = append(references, entry)
	}
	config["referenceImages"] = references
	report.ItemsWithReferenceImages++
	report.ReferenceImages += len(references)
	return nil
}

// fillOmniImageList mirrors omniConfig.imageList: inputImages.slice(1) on the
// web path. The provider payload for kling-v3-omni is inputImageUrl plus
// omniConfig.imageList (KlingOmniVideoModeAdapter.addImageList), and the backend
// resolves an imageOosKey-only entry to a URL before submitting, so both keys are
// carried exactly like the web client sends them.
func fillOmniImageList(item map[string]any, extras []material, index int, report *Report) error {
	if len(extras) == 0 {
		return nil
	}
	config, err := omniConfig(item, index)
	if err != nil {
		return err
	}
	if len(usableEntries(config["imageList"], omniImageKeys)) > 0 {
		return nil
	}
	references := make([]any, 0, len(extras))
	for _, extra := range extras {
		entry := map[string]any{}
		if extra.OosKey != "" {
			entry["imageOosKey"] = extra.OosKey
		}
		if extra.URL != "" {
			entry["imageUrl"] = extra.URL
		}
		references = append(references, entry)
	}
	config["imageList"] = references
	report.ItemsWithReferenceImages++
	report.ReferenceImages += len(references)
	return nil
}

func omniConfig(item map[string]any, index int) (map[string]any, error) {
	existing, present := item["omniConfig"]
	if !present || existing == nil {
		config := map[string]any{}
		item["omniConfig"] = config
		return config, nil
	}
	config, ok := existing.(map[string]any)
	if !ok {
		return nil, clierr.Usage("items[%d].omniConfig 必须是 JSON Object", index)
	}
	return config, nil
}

func seedanceConfig(item map[string]any, index int) (map[string]any, error) {
	existing, present := item["seedanceConfig"]
	if !present || existing == nil {
		config := map[string]any{}
		item["seedanceConfig"] = config
		return config, nil
	}
	config, ok := existing.(map[string]any)
	if !ok {
		return nil, clierr.Usage("items[%d].seedanceConfig 必须是 JSON Object", index)
	}
	return config, nil
}

// materialList keeps the usable inputImages entries in order; entries without a
// URL and without an OOS key cannot be submitted and are dropped, exactly like
// the web client's normalizeStandaloneInputImages filter.
func materialList(value any) []material {
	entries, ok := value.([]any)
	if !ok {
		return nil
	}
	materials := make([]material, 0, len(entries))
	for _, entry := range entries {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		candidate := material{
			OosKey: stringField(object, "oosKey"),
			URL:    stringField(object, "url"),
			Meta:   object["metadata"],
		}
		if candidate.OosKey == "" && candidate.URL == "" {
			continue
		}
		materials = append(materials, candidate)
	}
	return materials
}

// usableEntries reports the entries of an existing provider list that would
// actually be submitted; a hand-written list is respected as-is, so the mapping
// never overwrites or appends to one the caller already shaped.
func usableEntries(value any, keys [2]string) []any {
	entries, ok := value.([]any)
	if !ok {
		return nil
	}
	usable := make([]any, 0, len(entries))
	for _, entry := range entries {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if stringField(object, keys[0]) == "" && stringField(object, keys[1]) == "" {
			continue
		}
		usable = append(usable, entry)
	}
	return usable
}

func isSeedanceModel(code string) bool {
	return seedanceModelCodes[strings.ToUpper(strings.TrimSpace(code))]
}

func isOmniModel(code string) bool {
	return strings.EqualFold(strings.TrimSpace(code), omniModelCode)
}

func stringField(object map[string]any, key string) string {
	value, ok := object[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

// decodeObject preserves number literals via json.Number so int64 ids and
// durations survive a rewrite without float64 rounding.
func decodeObject(raw []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil {
		return nil, clierr.Usage("请求不是合法 JSON: %v", err)
	}
	return doc, nil
}

// encodeObject re-serializes compactly with HTML escaping off, so URLs carrying
// & or < keep the byte shape the caller wrote.
func encodeObject(doc map[string]any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(doc); err != nil {
		return nil, clierr.Usage("序列化归一化后的请求失败: %v", err)
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}
