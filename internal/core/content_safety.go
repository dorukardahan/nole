package core

import (
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
)

// ContentRisk describes deterministic indicators found in untrusted web data.
// It is not a verdict that content is safe or malicious.
type ContentRisk string

const (
	ContentRiskNoIndicators ContentRisk = "no_indicators"
	ContentRiskCaution      ContentRisk = "caution"
	ContentRiskHigh         ContentRisk = "high"
)

// ContentSafetySignal is deliberately payload-free. Returning the matched text
// would repeat a possible prompt injection in a second, higher-trust-looking
// field and could leak sensitive material embedded in a fetched page.
type ContentSafetySignal struct {
	Type     string      `json:"type"`
	Severity ContentRisk `json:"severity"`
	Count    int         `json:"count"`
}

// ContentSafetyReport travels with every search result and extract. Untrusted is
// always true: no_indicators means only that the deterministic scanner found no
// known indicator, never that the remote content is safe to follow as an
// instruction. Sanitized means invisible control characters were removed.
type ContentSafetyReport struct {
	Untrusted bool                  `json:"untrusted"`
	Risk      ContentRisk           `json:"risk"`
	Sanitized bool                  `json:"sanitized,omitempty"`
	Signals   []ContentSafetySignal `json:"signals,omitempty"`
}

var (
	instructionOverridePattern  = regexp.MustCompile(`(?i)\b(ignore|disregard|override|forget)\s+(all\s+|any\s+|the\s+)?(previous|prior|above|earlier|system|developer)\s+(instructions?|messages?|rules?|prompts?)\b`)
	sensitiveDataRequestPattern = regexp.MustCompile(`(?is)\b(reveal|show|print|return|expose|send|upload|exfiltrate)\b.{0,80}\b(system\s+prompt|developer\s+message|hidden\s+instructions?|secrets?|credentials?|api[_ -]?keys?|tokens?)\b`)
	toolRequestPattern          = regexp.MustCompile(`(?is)\b(call|invoke|execute|run|use)\b.{0,40}\b(tool|command|shell|terminal|browser|function)\b`)
	encodedPayloadPattern       = regexp.MustCompile(`\b[A-Za-z0-9+/]{80,}={0,2}\b|\b[0-9A-Fa-f]{120,}\b`)
	cssImportantPattern         = regexp.MustCompile(`(?i)\s*!\s*important\s*$`)
	cssNumberPattern            = regexp.MustCompile(`^([+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?)([a-z%]*)$`)
)

type safetyAccumulator map[string]ContentSafetySignal

func (a safetyAccumulator) add(kind string, severity ContentRisk, count int) {
	if count <= 0 {
		return
	}
	current := a[kind]
	current.Type = kind
	current.Count += count
	if riskRank(severity) > riskRank(current.Severity) {
		current.Severity = severity
	}
	a[kind] = current
}

func (a safetyAccumulator) report(sanitized bool) ContentSafetyReport {
	report := ContentSafetyReport{Untrusted: true, Risk: ContentRiskNoIndicators, Sanitized: sanitized}
	if len(a) == 0 {
		return report
	}
	keys := make([]string, 0, len(a))
	for key := range a {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		signal := a[key]
		report.Signals = append(report.Signals, signal)
		if riskRank(signal.Severity) > riskRank(report.Risk) {
			report.Risk = signal.Severity
		}
	}
	return report
}

// ProtectUntrustedText removes dangerous invisible controls, then scans the
// normalized text for deterministic prompt-injection indicators. Visible prose
// is never deleted or rewritten: security documentation may legitimately quote
// an attack. Pattern findings are presence signals (count=1), not occurrence
// totals, so attacker-controlled repetition cannot force a large match-index
// allocation.
func ProtectUntrustedText(input string) (string, ContentSafetyReport) {
	cleaned, suspiciousZeroWidth, bidi, controls, invalidUTF8, removed := sanitizeInvisibleControls(input)
	acc := safetyAccumulator{}

	originalInstruction := instructionOverridePattern.MatchString(input)
	originalSecret := sensitiveDataRequestPattern.MatchString(input)
	originalTool := toolRequestPattern.MatchString(input)
	cleanedInstruction := instructionOverridePattern.MatchString(cleaned)
	cleanedSecret := sensitiveDataRequestPattern.MatchString(cleaned)
	cleanedTool := toolRequestPattern.MatchString(cleaned)

	acc.add("zero_width_obfuscation", ContentRiskCaution, suspiciousZeroWidth)
	acc.add("bidi_control", ContentRiskCaution, bidi)
	acc.add("control_character", ContentRiskCaution, controls)
	acc.add("invalid_utf8", ContentRiskCaution, invalidUTF8)
	acc.add("instruction_override", ContentRiskHigh, boolCount(cleanedInstruction))
	acc.add("prompt_or_secret_request", ContentRiskHigh, boolCount(cleanedSecret))
	acc.add("tool_execution_request", ContentRiskCaution, boolCount(cleanedTool))
	if (cleanedInstruction && !originalInstruction) || (cleanedSecret && !originalSecret) || (cleanedTool && !originalTool) {
		acc.add("invisible_instruction_obfuscation", ContentRiskHigh, 1)
	}

	confusableNormalized := normalizeCommonCyrillicConfusables(cleaned)
	if confusableNormalized != cleaned {
		normalizedDanger := instructionOverridePattern.MatchString(confusableNormalized) || sensitiveDataRequestPattern.MatchString(confusableNormalized)
		cleanedDanger := cleanedInstruction || cleanedSecret
		if normalizedDanger && !cleanedDanger {
			acc.add("homoglyph_instruction_obfuscation", ContentRiskHigh, 1)
		}
	}
	acc.add("encoded_payload", ContentRiskCaution, boolCount(encodedPayloadPattern.MatchString(cleaned)))
	acc.add("mixed_script_token", ContentRiskCaution, countMixedScriptTokens(cleaned))
	return cleaned, acc.report(removed > 0)
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

// sanitizeInvisibleControls is two-pass and bounded-memory. Pass one removes
// unconditional controls; pass two evaluates ZWNJ/ZWJ runs against the nearest
// surviving neighbours. Each pass allocates only if it changes the string.
func sanitizeInvisibleControls(input string) (cleaned string, suspiciousZeroWidth, bidi, controls, invalidUTF8, removed int) {
	normalized := input
	var first strings.Builder
	firstDirty := false
	beginFirst := func(offset int) {
		if firstDirty {
			return
		}
		firstDirty = true
		first.Grow(len(input))
		first.WriteString(input[:offset])
	}

	for offset := 0; offset < len(input); {
		r, size := utf8.DecodeRuneInString(input[offset:])
		if r == utf8.RuneError && size == 1 {
			beginFirst(offset)
			first.WriteRune(utf8.RuneError)
			invalidUTF8++
			removed++
			offset += size
			continue
		}

		remove := false
		suspicious := false
		switch {
		case (r >= 0 && r < 0x20 && r != '	' && r != '\n' && r != '\r') || (r >= 0x7f && r <= 0x9f):
			remove = true
			controls++
		case r == '\ufeff':
			remove = true
			suspicious = offset != 0
		case r == '\u00ad':
			remove = true
		case r == '\u034f' || r == '\u180e' || r == '\u200b' || r == '\u2060' || (r >= '\u2061' && r <= '\u2064') || (r >= '\u206a' && r <= '\u206f'):
			remove = true
			suspicious = true
		case r == '\u061c' || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') || r == '\u200e' || r == '\u200f':
			remove = true
			bidi++
		}
		if remove {
			beginFirst(offset)
			removed++
			if suspicious {
				suspiciousZeroWidth++
			}
		} else if firstDirty {
			first.WriteString(input[offset : offset+size])
		}
		offset += size
	}
	if firstDirty {
		normalized = first.String()
	}

	var second strings.Builder
	secondDirty := false
	var previous rune
	for offset := 0; offset < len(normalized); {
		r, size := utf8.DecodeRuneInString(normalized[offset:])
		if r == '\u200c' || r == '\u200d' {
			runEnd := offset
			runCount := 0
			for runEnd < len(normalized) {
				runRune, runSize := utf8.DecodeRuneInString(normalized[runEnd:])
				if runRune != '\u200c' && runRune != '\u200d' {
					break
				}
				runEnd += runSize
				runCount++
			}
			afterRun, _ := utf8.DecodeRuneInString(normalized[runEnd:])
			if latinLetter(previous) && latinLetter(afterRun) {
				if !secondDirty {
					secondDirty = true
					second.Grow(len(normalized))
					second.WriteString(normalized[:offset])
				}
				suspiciousZeroWidth += runCount
				removed += runCount
				offset = runEnd
				continue
			}
			// A legitimate/non-Latin joiner run is preserved, but consume the
			// whole run at once so attacker-controlled runs remain linear-time.
			if secondDirty {
				second.WriteString(normalized[offset:runEnd])
			}
			previous, _ = utf8.DecodeLastRuneInString(normalized[offset:runEnd])
			offset = runEnd
			continue
		}
		if secondDirty {
			second.WriteString(normalized[offset : offset+size])
		}
		previous = r
		offset += size
	}
	if secondDirty {
		normalized = second.String()
	}
	return normalized, suspiciousZeroWidth, bidi, controls, invalidUTF8, removed
}

func latinLetter(r rune) bool {
	return unicode.IsLetter(r) && unicode.In(r, unicode.Latin)
}

func normalizeCommonCyrillicConfusables(text string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case 'а', 'А':
			return 'a'
		case 'е', 'Е':
			return 'e'
		case 'о', 'О':
			return 'o'
		case 'р', 'Р':
			return 'p'
		case 'с', 'С':
			return 'c'
		case 'х', 'Х':
			return 'x'
		case 'у', 'У':
			return 'y'
		case 'і', 'І':
			return 'i'
		case 'ј', 'Ј':
			return 'j'
		case 'ѕ', 'Ѕ':
			return 's'
		default:
			return r
		}
	}, text)
}

// ScanRawHTMLContentSafety inspects channels a readable-text extractor normally
// removes. It uses the same HTML parser as httpfetch's readable-text pass, so
// arbitrary tags, nested same-name elements and quoted '>' characters cannot
// bypass the hidden-node boundary. Reports contain counts and fixed names only.
func ScanRawHTMLContentSafety(raw []byte) ContentSafetyReport {
	doc, err := xhtml.Parse(bytes.NewReader(raw))
	if err != nil {
		return ContentSafetyReport{Untrusted: true, Risk: ContentRiskNoIndicators}
	}
	acc := safetyAccumulator{}
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.CommentNode {
			if instructionOverridePattern.MatchString(node.Data) || sensitiveDataRequestPattern.MatchString(node.Data) || toolRequestPattern.MatchString(node.Data) {
				acc.add("html_comment_instruction", ContentRiskHigh, 1)
			}
			return
		}
		if kind := HTMLNodeHiddenKind(node); kind != "" {
			if !subtreeHasContent(node) {
				return
			}
			acc.add(kind, ContentRiskCaution, 1)
			if subtreeHasInstruction(node) {
				acc.add(kind+"_instruction", ContentRiskHigh, 1)
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return acc.report(false)
}

// HTMLNodeHiddenKind returns the payload-free observation type for a DOM element
// that browsers hide through an explicit attribute or inline style.
func HTMLNodeHiddenKind(node *xhtml.Node) string {
	if node == nil || node.Type != xhtml.ElementNode {
		return ""
	}
	switch strings.ToLower(node.Data) {
	case "head", "script", "style", "noscript", "template", "svg", "title":
		return "nonvisible_html"
	}
	for _, attr := range node.Attr {
		name := strings.ToLower(strings.TrimSpace(attr.Key))
		value := strings.TrimSpace(attr.Val)
		switch name {
		case "hidden":
			return "hidden_html"
		case "aria-hidden":
			if strings.EqualFold(value, "true") {
				return "hidden_html"
			}
		case "style":
			if inlineStyleHidesContent(value) {
				return "css_hidden_content"
			}
		}
	}
	return ""
}

func inlineStyleHidesContent(style string) bool {
	for _, declaration := range splitCSSDeclarations(style) {
		parts := strings.SplitN(declaration, ":", 2)
		if len(parts) != 2 {
			continue
		}
		property := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.ToLower(strings.TrimSpace(parts[1]))
		value = strings.TrimSpace(cssImportantPattern.ReplaceAllString(value, ""))
		switch property {
		case "display":
			if value == "none" {
				return true
			}
		case "visibility":
			if value == "hidden" || value == "collapse" {
				return true
			}
		case "opacity":
			if cssNumberIsZero(value, "%") {
				return true
			}
		case "font-size":
			if cssNumberIsZero(value, "px", "em", "rem", "%", "pt") {
				return true
			}
		}
	}
	return false
}

// splitCSSDeclarations is a bounded scanner for inline declaration lists. It
// removes comments outside strings and splits only on top-level semicolons,
// preserving quoted/custom-property values and function arguments.
func splitCSSDeclarations(style string) []string {
	declarations := make([]string, 0, 4)
	var current strings.Builder
	var quote byte
	parenDepth := 0
	for i := 0; i < len(style); {
		ch := style[i]
		if quote != 0 {
			current.WriteByte(ch)
			if ch == '\\' && i+1 < len(style) {
				current.WriteByte(style[i+1])
				i += 2
				continue
			}
			if ch == quote {
				quote = 0
			}
			i++
			continue
		}
		if ch == '/' && i+1 < len(style) && style[i+1] == '*' {
			i += 2
			for i+1 < len(style) && !(style[i] == '*' && style[i+1] == '/') {
				i++
			}
			if i+1 < len(style) {
				i += 2
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			current.WriteByte(ch)
			i++
			continue
		}
		if ch == '\\' && i+1 < len(style) {
			current.WriteByte(ch)
			current.WriteByte(style[i+1])
			i += 2
			continue
		}
		switch ch {
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case ';':
			if parenDepth == 0 {
				declarations = append(declarations, strings.Clone(current.String()))
				current.Reset()
				i++
				continue
			}
		}
		current.WriteByte(ch)
		i++
	}
	declarations = append(declarations, strings.Clone(current.String()))
	return declarations
}

func cssNumberIsZero(value string, units ...string) bool {
	matches := cssNumberPattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(value)))
	if matches == nil {
		return false
	}
	unit := matches[2]
	if unit != "" {
		allowed := false
		for _, candidate := range units {
			if unit == candidate {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	number, err := strconv.ParseFloat(matches[1], 64)
	return err == nil && number == 0
}

func subtreeHasContent(node *xhtml.Node) bool {
	if node == nil {
		return false
	}
	if (node.Type == xhtml.TextNode || node.Type == xhtml.CommentNode) && strings.TrimSpace(node.Data) != "" {
		return true
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if subtreeHasContent(child) {
			return true
		}
	}
	return false
}

func subtreeHasInstruction(node *xhtml.Node) bool {
	if node == nil {
		return false
	}
	if (node.Type == xhtml.TextNode || node.Type == xhtml.CommentNode) && (instructionOverridePattern.MatchString(node.Data) || sensitiveDataRequestPattern.MatchString(node.Data) || toolRequestPattern.MatchString(node.Data)) {
		return true
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if subtreeHasInstruction(child) {
			return true
		}
	}
	return false
}

// MergeContentSafety combines provider-level raw-document signals with the
// central normalized-text scan. Signal order is deterministic and duplicate
// types are collapsed.
func MergeContentSafety(reports ...ContentSafetyReport) ContentSafetyReport {
	acc := safetyAccumulator{}
	sanitized := false
	highest := ContentRiskNoIndicators
	for _, report := range reports {
		sanitized = sanitized || report.Sanitized
		if riskRank(report.Risk) > riskRank(highest) {
			highest = report.Risk
		}
		for _, signal := range report.Signals {
			severity := signal.Severity
			if severity == "" {
				severity = report.Risk
			}
			acc.add(signal.Type, severity, signal.Count)
		}
	}
	merged := acc.report(sanitized)
	if riskRank(highest) > riskRank(merged.Risk) {
		merged.Risk = highest
	}
	return merged
}

func protectSearchResults(results []SearchResult) {
	for i := range results {
		title, titleReport := ProtectUntrustedText(results[i].Title)
		url, urlReport := ProtectUntrustedText(results[i].URL)
		snippet, snippetReport := ProtectUntrustedText(results[i].Snippet)
		publishedAt, publishedAtReport := ProtectUntrustedText(results[i].PublishedAt)
		results[i].Title = title
		results[i].URL = url
		results[i].Snippet = snippet
		results[i].PublishedAt = publishedAt
		results[i].ContentSafety = MergeContentSafety(results[i].ContentSafety, titleReport, urlReport, snippetReport, publishedAtReport)
	}
}

func ensureProtectedSearchResults(results []SearchResult) {
	for i := range results {
		if contentSafetyInitialized(results[i].ContentSafety) {
			continue
		}
		protectSearchResults(results[i : i+1])
	}
}

func contentSafetyInitialized(report ContentSafetyReport) bool {
	return report.Untrusted && report.Risk != ""
}

func protectExtractMetadata(metadata map[string]string) ContentSafetyReport {
	if len(metadata) == 0 {
		return ContentSafetyReport{Untrusted: true, Risk: ContentRiskNoIndicators}
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	rebuilt := make(map[string]string, len(metadata))
	merged := ContentSafetyReport{Untrusted: true, Risk: ContentRiskNoIndicators}
	collisions := 0
	for _, originalKey := range keys {
		cleanedKey, keyReport := ProtectUntrustedText(originalKey)
		cleanedValue, valueReport := ProtectUntrustedText(metadata[originalKey])
		base := cleanedKey
		if base == "" {
			base = "_metadata_key"
		}
		candidate := base
		for duplicate := 2; ; duplicate++ {
			if _, exists := rebuilt[candidate]; !exists {
				break
			}
			collisions++
			candidate = base + "__duplicate_" + strconv.Itoa(duplicate)
		}
		rebuilt[candidate] = cleanedValue
		merged = MergeContentSafety(merged, keyReport, valueReport)
	}
	for key := range metadata {
		delete(metadata, key)
	}
	for key, value := range rebuilt {
		metadata[key] = value
	}
	collisionSignals := safetyAccumulator{}
	collisionSignals.add("metadata_key_collision", ContentRiskCaution, collisions)
	return MergeContentSafety(merged, collisionSignals.report(false))
}

func countMixedScriptTokens(text string) int {
	count := 0
	latin := false
	cyrillic := false
	flush := func() {
		if latin && cyrillic {
			count++
		}
		latin = false
		cyrillic = false
	}
	for _, r := range text {
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		if unicode.In(r, unicode.Latin) {
			latin = true
		}
		if unicode.In(r, unicode.Cyrillic) {
			cyrillic = true
		}
	}
	flush()
	return count
}

func riskRank(risk ContentRisk) int {
	switch risk {
	case ContentRiskHigh:
		return 2
	case ContentRiskCaution:
		return 1
	default:
		return 0
	}
}

func cloneContentSafety(report ContentSafetyReport) ContentSafetyReport {
	report.Signals = append([]ContentSafetySignal(nil), report.Signals...)
	return report
}
