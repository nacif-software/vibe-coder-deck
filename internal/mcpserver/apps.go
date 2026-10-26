package mcpserver

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nacif-software/vibe-coder-deck/internal/deck"
)

// MCP Apps (SEP-1865, extension io.modelcontextprotocol/ui): show_pad links
// to a ui:// resource that hosts with the extension render as an interactive
// view of the pad. Hosts without it ignore _meta.ui and get the text.

const (
	padViewURI  = "ui://vibe-coder-deck/pad"
	appMIMEType = "text/html;profile=mcp-app"
)

//go:embed ui/pad.html
var padViewTemplate string

// padViewHTML is the view with the built-in icon drawings inlined: the view
// runs sandboxed in the host and cannot fetch anything.
var padViewHTML = func() string {
	icons, _ := json.Marshal(deck.Icons())
	return strings.Replace(padViewTemplate, "/*ICONS*/[]", string(icons), 1)
}()

// The view needs nothing outside itself: no network, no external assets, so
// it runs under the host's restrictive default CSP.
var padViewMeta = mcp.Meta{"ui": map[string]any{"prefersBorder": false}}

func addApps(s *mcp.Server, d Deps) {
	s.AddResource(&mcp.Resource{
		URI:         padViewURI,
		Name:        "pad",
		Title:       "Vibe Coder Deck pad",
		Description: "Interactive view of the macropad: its six keys and knob, what each does, and an editor that saves a key and writes the pad.",
		MIMEType:    appMIMEType,
		Meta:        padViewMeta,
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI: padViewURI, MIMEType: appMIMEType, Text: padViewHTML, Meta: padViewMeta,
		}}}, nil
	})
	mcp.AddTool(s, &mcp.Tool{
		Name:        "show_pad",
		Title:       "Show the pad",
		Description: "Show the pad to the user as an interactive view (in hosts with MCP Apps): the keys and knob as they look, what each does; the user can pick a profile and a control, change it, save it and write the pad from there. Returns the same data as list_keys.",
		Meta:        mcp.Meta{"ui": map[string]any{"resourceUri": padViewURI}},
	}, d.showPad)
}

func (d Deps) showPad(ctx context.Context, req *mcp.CallToolRequest, in profileIn) (*mcp.CallToolResult, keysOut, error) {
	_, out, err := d.listKeys(ctx, req, in)
	if err != nil {
		return nil, out, err
	}
	// The text is what the model and hosts without MCP Apps see.
	var b strings.Builder
	fmt.Fprintf(&b, "Profile: %s\n", d.profileLabel(out.ProfileID))
	for _, k := range out.Keys {
		what := k.Action
		if k.Mode == "macro" {
			what = fmt.Sprintf("%d steps", len(k.Steps))
		}
		label := ""
		if k.Label != "" {
			label = fmt.Sprintf(" %q", k.Label)
		}
		fmt.Fprintf(&b, "%s%s: %s %s\n", k.Slot, label, k.Mode, what)
	}
	if out.PadBehind {
		b.WriteString("The pad is behind this configuration: call sync_pad.\n")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.TrimSpace(b.String())}}}, out, nil
}
