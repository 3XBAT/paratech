package doctext

import (
	"context"
	"encoding/xml"
	"fmt"
	"path"
	"strings"

	"paratech/desktop/internal/domain/library/material"
)

// pptx reads the slides in the order of the presentation, one fragment per slide.
func pptx(ctx context.Context, data []byte) ([]material.Fragment, bool, error) {
	p, err := open(data)
	if err != nil {
		return nil, false, err
	}
	order, err := slideOrder(ctx, p)
	if err != nil {
		return nil, false, err
	}
	var (
		fs   []material.Fragment
		size int
	)
	for i, name := range order {
		if i == material.MaxFragments {
			return fs, true, nil
		}
		body, err := p.part(name)
		if err != nil {
			return nil, false, err
		}
		w := newTextWriter()
		w.size = size // the limit counts the whole presentation
		if err := slideText(ctx, body, w); err != nil {
			return nil, false, err
		}
		fs = append(fs, material.Fragment{Locator: material.Locator{Kind: material.LocatorSlide, N: i + 1}, Lines: w.lines()})
		size = w.size
		if w.truncated {
			return fs, true, nil
		}
	}
	return fs, false, nil
}

// slideText collects the DrawingML paragraphs of a slide: text runs (a:t); a line break (a:br) starts a new line.
func slideText(ctx context.Context, body []byte, w *textWriter) error {
	inText := 0
	err := walk(ctx, body,
		func(s xml.StartElement, _ *xml.Decoder) error {
			switch s.Name.Local {
			case "p", "br":
				w.endParagraph()
			case "t":
				inText++
			}
			return nil
		},
		func(e xml.EndElement) {
			switch e.Name.Local {
			case "p":
				w.endParagraph()
			case "t":
				inText = max(inText-1, 0)
			}
		},
		func(c xml.CharData) {
			if inText > 0 {
				w.write(string(c))
			}
		})
	w.endParagraph()
	return err
}

// slideOrder returns the part names of the slides in the order of the presentation's slide list (p:sldIdLst), each
// resolved through the relationships of the presentation part.
func slideOrder(ctx context.Context, p *pkg) ([]string, error) {
	main, err := p.part(pptxMain)
	if err != nil {
		return nil, err
	}
	var ids []string
	inList := false
	err = walk(ctx, main,
		func(s xml.StartElement, _ *xml.Decoder) error {
			switch s.Name.Local {
			case "sldIdLst":
				inList = true
			case "sldId":
				if inList {
					// The relationship id is the r:id attribute: "id" in a namespace (the plain id is a number).
					for _, a := range s.Attr {
						if a.Name.Local == "id" && a.Name.Space != "" {
							ids = append(ids, a.Value)
						}
					}
				}
			}
			return nil
		},
		func(e xml.EndElement) {
			if e.Name.Local == "sldIdLst" {
				inList = false
			}
		},
		func(xml.CharData) {})
	if err != nil {
		return nil, err
	}

	rels, err := p.part(pptxMainRels)
	if err != nil {
		return nil, err
	}
	targets := map[string]string{}
	err = walk(ctx, rels,
		func(s xml.StartElement, _ *xml.Decoder) error {
			if s.Name.Local == "Relationship" && attr(s, "TargetMode") != "External" {
				targets[attr(s, "Id")] = attr(s, "Target")
			}
			return nil
		},
		func(xml.EndElement) {}, func(xml.CharData) {})
	if err != nil {
		return nil, err
	}

	order := make([]string, 0, len(ids))
	for _, id := range ids {
		name, ok := slidePart(targets[id])
		if !ok {
			return nil, fmt.Errorf("a slide relationship points outside the presentation")
		}
		order = append(order, name)
	}
	return order, nil
}

// slidePart resolves a relationship target of the presentation part to an entry name under ppt/; anything that
// leaves ppt/ or is not an XML part is refused. The name is only looked up in the package, never opened as a path.
func slidePart(target string) (string, bool) {
	if target == "" || strings.ContainsAny(target, "\\\x00") {
		return "", false
	}
	var name string
	if strings.HasPrefix(target, "/") {
		name = path.Clean(strings.TrimPrefix(target, "/"))
	} else {
		name = path.Clean(pptxDir + target)
	}
	if !strings.HasPrefix(name, pptxDir) || !strings.HasSuffix(name, ".xml") {
		return "", false
	}
	return name, true
}
