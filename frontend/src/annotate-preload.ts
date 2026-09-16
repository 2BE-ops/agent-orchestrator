import { ipcRenderer } from "electron";
import geistLatinWoff2 from "@fontsource-variable/geist/files/geist-latin-wght-normal.woff2?inline";
import geistMonoLatinWoff2 from "@fontsource-variable/geist-mono/files/geist-mono-latin-wght-normal.woff2?inline";
import {
	createBrowserAnnotationContext,
	createBrowserAnnotationSession,
	type BrowserAdjustmentProperty,
	type BrowserAnnotationCancelReason,
	type BrowserAnnotationPageMode,
	type BrowserAnnotationTarget,
	type BrowserAnnotationTheme,
	type BrowserSavedAnnotation,
	type BrowserStyleAdjustment,
} from "./shared/browser-annotations";
import { promptPositionForRect, type AnnotationRectLike } from "./shared/browser-annotation-overlay";

let enabled = false;
let session = createBrowserAnnotationSession(window.location.href, document.title || undefined);
let hoveredElement: Element | null = null;
let selectedElement: Element | null = null;
let host: HTMLDivElement | null = null;
let shadow: ShadowRoot | null = null;
let theme: BrowserAnnotationTheme = {};
let showingOriginal = false;
let adjustmentBaseline = new WeakMap<Element, Map<BrowserAdjustmentProperty, string>>();
let editableTextTargets = new WeakMap<Element, Text>();
let composerAnchorRect: AnnotationRectLike | null = null;
type AdjustmentLockName = "ratio" | "paddingHorizontal" | "paddingVertical" | "marginHorizontal" | "marginVertical";
const emptyAdjustmentLocks = () => ({ ratio: false, paddingHorizontal: false, paddingVertical: false, marginHorizontal: false, marginVertical: false, ratioValue: 0 });
let adjustmentLocks = emptyAdjustmentLocks();
let nextLocalId = 0;
let screenshotNoticeTimer: ReturnType<typeof setTimeout> | null = null;

const ADJUST_MODE_ENABLED = true;

const PROMPT_GUTTER = 14;
const PROMPT_GAP = 10;
const COMMENT_WIDTH = 320;
const ADJUSTMENT_WIDTH = 316;
const ADJUSTMENT_MAX_HEIGHT = 400;
const COMMENT_TEXTAREA_MIN_HEIGHT = 20;
const COMMENT_MAX_HEIGHT = 360;
/** Keep in sync with overlayStyles --pad / --gap / --control / --radius. */
const COMPOSER_PAD = 8;
const COMPOSER_GAP = 8;
const COMPOSER_CONTROL = 28;
const COMPOSER_RADIUS = 8;
const COMMENT_RADIUS = 16;
const COMMENT_CHROME_VERTICAL = 16;
/** Icon for the adjust/options control: sliders | sliders-horizontal | tune | sparkles | settings */
const ADJUST_ICON = "sliders-horizontal" as const;
/** Extra pixels outside the target on each side while a selection is active. */
const SELECTED_OUTSET_PX = 6;
const MARKDOWN_TARGETS =
	"h1, h2, h3, h4, h5, h6, p, ul, ol, li, blockquote, pre, table, th, td, figure, figcaption, img, hr, details, summary";

type AdjustmentDefinition = {
	property: BrowserAdjustmentProperty;
	label: string;
	group: "Content" | "Appearance" | "Size" | "Spacing" | "Layout";
	options?: string[];
};

const ADJUSTMENTS: AdjustmentDefinition[] = [
	{ property: "textContent", label: "Text", group: "Content" },
	{ property: "color", label: "Text color", group: "Appearance" },
	{ property: "backgroundColor", label: "Background", group: "Appearance" },
	{ property: "opacity", label: "Opacity", group: "Appearance" },
	{ property: "fontFamily", label: "Font", group: "Appearance", options: ["system-ui", "Arial", "Helvetica", "Georgia", "Times New Roman", "monospace"] },
	{ property: "fontSize", label: "Font size", group: "Appearance" },
	{ property: "fontWeight", label: "Font weight", group: "Appearance", options: ["100", "200", "300", "400", "500", "600", "700", "800", "900"] },
	{ property: "borderRadius", label: "Border radius", group: "Appearance" },
	{ property: "borderColor", label: "Border color", group: "Appearance" },
	{ property: "borderWidth", label: "Border width", group: "Appearance" },
	{ property: "width", label: "Width", group: "Size" },
	{ property: "height", label: "Height", group: "Size" },
	{ property: "paddingTop", label: "Top", group: "Spacing" },
	{ property: "paddingBottom", label: "Bottom", group: "Spacing" },
	{ property: "paddingLeft", label: "Left", group: "Spacing" },
	{ property: "paddingRight", label: "Right", group: "Spacing" },
	{ property: "marginTop", label: "Top", group: "Spacing" },
	{ property: "marginBottom", label: "Bottom", group: "Spacing" },
	{ property: "marginLeft", label: "Left", group: "Spacing" },
	{ property: "marginRight", label: "Right", group: "Spacing" },
	{ property: "flexDirection", label: "Flex direction", group: "Layout", options: ["row", "row-reverse", "column", "column-reverse"] },
	{ property: "alignItems", label: "Alignment", group: "Layout", options: ["start", "center", "end", "stretch", "baseline"] },
	{ property: "justifyContent", label: "Distribution", group: "Layout", options: ["start", "center", "end", "space-between", "space-around", "space-evenly"] },
	{ property: "columnGap", label: "Horizontal gap", group: "Layout" },
	{ property: "rowGap", label: "Vertical gap", group: "Layout" },
];

const PIXEL_PROPERTIES = new Set<BrowserAdjustmentProperty>([
	"fontSize", "borderWidth", "borderRadius", "width", "height",
	"marginTop", "marginRight", "marginBottom", "marginLeft",
	"paddingTop", "paddingRight", "paddingBottom", "paddingLeft",
	"columnGap", "rowGap",
]);
const COLOR_PROPERTIES = new Set<BrowserAdjustmentProperty>(["color", "backgroundColor", "borderColor"]);
const TEXT_STYLE_PROPERTIES = new Set<BrowserAdjustmentProperty>(["color", "fontFamily", "fontSize", "fontWeight"]);
const SUPPORTED_ADJUSTMENT_PROPERTIES = new Set(ADJUSTMENTS.map((item) => item.property));

ipcRenderer.on("browser:annotation:setMode", (_event, input: BrowserAnnotationPageMode) => {
	if (input?.theme) theme = input.theme;
	const nextEnabled = Boolean(input?.enabled);
	// Clear any in-progress composer before applying the stored session when
	// leaving mode, so a stale draft from main cannot reopen the input.
	if (!nextEnabled) clearOpenDraft();
	if (input?.session && samePage(input.session.page.url, window.location.href)) session = input.session;
	else if (!samePage(session.page.url, window.location.href)) session = createBrowserAnnotationSession(window.location.href, document.title || undefined);
	if (!nextEnabled && session.draft) {
		delete session.draft;
		emitState();
	}
	sanitizeSessionAdjustments();
	applyAllAdjustments();
	setEnabled(nextEnabled, "disabled");
});

ipcRenderer.on("browser:annotation:action", (_event, action: string) => {
	if (!enabled) return;
	if (action === "capture") void captureScreenshot();
	else if (action === "preview-original") setOriginalPreview(true);
	else if (action === "restore-preview") setOriginalPreview(false);
	else if (action === "discard-all") discardAll();
	else if (action === "submit") void submitBatch();
});

window.addEventListener("beforeunload", cleanupOverlay);

function sanitizeSessionAdjustments(): void {
	for (const annotation of session.annotations)
		annotation.adjustments = annotation.adjustments.filter((item) => SUPPORTED_ADJUSTMENT_PROPERTIES.has(item.property));
	if (session.draft)
		session.draft.adjustments = session.draft.adjustments.filter((item) => SUPPORTED_ADJUSTMENT_PROPERTIES.has(item.property));
}

function setEnabled(next: boolean, reason: BrowserAnnotationCancelReason): void {
	if (enabled === next) {
		if (next) renderAll();
		return;
	}
	// Leaving mode drops any open composer/selection. Saved batch annotations
	// stay on the session. Main also strips draft on disable so re-entry does
	// not restore a selection; reload-while-enabled can still rehydrate draft.
	if (!next) clearOpenDraft();
	enabled = next;
	hoveredElement = null;
	selectedElement = null;
	composerAnchorRect = null;
	if (next) {
		if (session.draft) {
			selectedElement = resolveTarget(session.draft.target);
			composerAnchorRect = selectedElement ? copyRect(selectedElement.getBoundingClientRect()) : null;
		}
		ensureOverlay();
		installListeners();
		renderAll();
	} else {
		removeListeners();
		cleanupOverlay();
		if (reason !== "disabled") ipcRenderer.send("browser:annotation:cancel", { reason });
	}
}

/** Close an in-progress composer without removing saved batch annotations. */
function clearOpenDraft(): void {
	const draft = session.draft;
	if (!draft) return;
	const element = selectedElement ?? resolveTarget(draft.target);
	if (element) restoreAdjustments(element, draft.adjustments);
	if (draft.id) applyAllAdjustments();
	delete session.draft;
	adjustmentLocks = emptyAdjustmentLocks();
	composerAnchorRect = null;
	selectedElement = null;
	hoveredElement = null;
	emitState();
}

function installListeners(): void {
	document.addEventListener("pointermove", handlePointerMove, true);
	document.addEventListener("click", handleClick, true);
	document.addEventListener("keydown", handleKeyDown, true);
	window.addEventListener("scroll", refreshPositions, true);
	window.addEventListener("resize", refreshPositions, true);
}

function removeListeners(): void {
	document.removeEventListener("pointermove", handlePointerMove, true);
	document.removeEventListener("click", handleClick, true);
	document.removeEventListener("keydown", handleKeyDown, true);
	window.removeEventListener("scroll", refreshPositions, true);
	window.removeEventListener("resize", refreshPositions, true);
}

function handlePointerMove(event: PointerEvent): void {
	if (!enabled || isOverlayEvent(event) || session.draft) return;
	const target = annotationTarget(event.target);
	if (target === hoveredElement) return;
	hoveredElement = target;
	renderHover();
}

function handleClick(event: MouseEvent): void {
	if (!enabled || isOverlayEvent(event)) return;
	if (session.draft) {
		event.preventDefault();
		event.stopImmediatePropagation();
		closeComposer();
		return;
	}
	const target = annotationTarget(event.target);
	if (!target) return;
	event.preventDefault();
	event.stopImmediatePropagation();
	openComposer(target);
}

function handleKeyDown(event: KeyboardEvent): void {
	if (!enabled || isOverlayEvent(event)) return;
	if (event.key === "Escape") {
		event.preventDefault();
		if (session.draft) closeComposer();
		else setEnabled(false, "escape");
	} else if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
		event.preventDefault();
		void submitBatch();
	}
}

function annotationTarget(target: EventTarget | null): Element | null {
	if (!(target instanceof Element)) return null;
	const element =
		target.closest("button, a, input, textarea, select, [role]") ??
		(target.closest(".markdown-body") ? target.closest(MARKDOWN_TARGETS) : null) ??
		target.closest("[data-testid], [id], [class]") ??
		target;
	return element === document.documentElement || element === document.body ? null : element;
}

function openComposer(element: Element, annotation?: BrowserSavedAnnotation): void {
	selectedElement = element;
	hoveredElement = null;
	composerAnchorRect = copyRect(element.getBoundingClientRect());
	adjustmentLocks = emptyAdjustmentLocks();
	const target: BrowserAnnotationTarget = annotation?.target ?? { context: createBrowserAnnotationContext(element) };
	session.draft = annotation
		? { id: annotation.id, kind: annotation.kind, body: annotation.body, target, adjustments: annotation.adjustments.map((item) => ({ ...item })) }
		: { kind: "comment", body: "", target, adjustments: [] };
	emitState();
	renderAll();
	setTimeout(() => shadow?.querySelector<HTMLTextAreaElement>(".composer-note")?.focus(), 0);
}

function closeComposer(): void {
	clearOpenDraft();
	renderAll();
}

function saveDraft(): boolean {
	const draft = session.draft;
	if (!draft) return true;
	if (!draft.body.trim() && draft.adjustments.length === 0) {
		if (draft.id) discardSelected();
		else closeComposer();
		return true;
	}
	const now = new Date().toISOString();
	const existing = draft.id ? session.annotations.find((item) => item.id === draft.id) : undefined;
	if (existing) {
		existing.kind = draft.kind;
		existing.body = draft.body.trim();
		existing.target = draft.target;
		existing.adjustments = draft.adjustments.map((item) => ({ ...item }));
		existing.updatedAt = now;
	} else {
		session.annotations.push({
			id: localId("annotation"),
			number: nextAnnotationNumber(),
			kind: draft.kind,
			body: draft.body.trim(),
			target: draft.target,
			adjustments: draft.adjustments.map((item) => ({ ...item })),
			createdAt: now,
			updatedAt: now,
		});
	}
	delete session.draft;
	adjustmentLocks = emptyAdjustmentLocks();
	composerAnchorRect = null;
	selectedElement = null;
	hoveredElement = null;
	emitState();
	renderAll();
	return true;
}

async function submitBatch(): Promise<void> {
	if (!saveDraft() || session.annotations.length === 0) return;
	const root = ensureOverlay();
	root.querySelectorAll<HTMLElement>(".chrome").forEach((item) => { item.hidden = true; });
	await waitForPaint();
	await ipcRenderer.invoke("browser:annotation:submit", { session });
	setEnabled(false, "disabled");
}

async function captureScreenshot(): Promise<void> {
	const root = ensureOverlay();
	root.querySelectorAll<HTMLElement>(".chrome").forEach((item) => { item.hidden = true; });
	await waitForPaint();
	const snapshot = await ipcRenderer.invoke("browser:annotation:capture");
	if (snapshot?.data && snapshot?.mimeType) {
		session.screenshots.push({ ...snapshot, id: localId("screenshot"), createdAt: new Date().toISOString() });
		emitState();
	}
	renderAll();
	showScreenshotNotice();
}

function discardSelected(): void {
	const draft = session.draft;
	if (!draft) return;
	if (selectedElement) restoreAdjustments(selectedElement, draft.adjustments);
	if (draft.id) session.annotations = session.annotations.filter((annotation) => annotation.id !== draft.id);
	delete session.draft;
	adjustmentLocks = emptyAdjustmentLocks();
	composerAnchorRect = null;
	selectedElement = null;
	hoveredElement = null;
	emitState();
	renderAll();
}

function discardAll(): void {
	for (const annotation of session.annotations) {
		const element = resolveTarget(annotation.target);
		if (element) restoreAdjustments(element, annotation.adjustments);
	}
	if (selectedElement && session.draft) restoreAdjustments(selectedElement, session.draft.adjustments);
	session = createBrowserAnnotationSession(window.location.href, document.title || undefined);
	selectedElement = null;
	hoveredElement = null;
	adjustmentBaseline = new WeakMap();
	editableTextTargets = new WeakMap();
	adjustmentLocks = emptyAdjustmentLocks();
	composerAnchorRect = null;
	ipcRenderer.send("browser:annotation:discard");
	emitState();
	renderAll();
}

function showScreenshotNotice(): void {
	const notice = ensureOverlay().querySelector<HTMLElement>(".screenshot-notice");
	if (!notice) return;
	notice.hidden = false;
	if (screenshotNoticeTimer) clearTimeout(screenshotNoticeTimer);
	screenshotNoticeTimer = setTimeout(() => {
		const current = shadow?.querySelector<HTMLElement>(".screenshot-notice");
		if (current) current.hidden = true;
		screenshotNoticeTimer = null;
	}, 1_600);
}

function emitState(): void {
	ipcRenderer.send("browser:annotation:state", session);
}

function renderAll(): void {
	if (!enabled) return;
	ensureOverlay();
	renderHover();
	renderMarkers();
	renderComposer();
}

function renderHover(): void {
	const highlight = ensureOverlay().querySelector<HTMLElement>(".hover");
	if (!highlight) return;
	const target = session.draft
		? (selectedElement ?? resolveTarget(session.draft.target))
		: hoveredElement;
	if (!target) {
		highlight.hidden = true;
		highlight.classList.remove("hover--selected");
		return;
	}
	const selecting = Boolean(session.draft);
	const rect = target.getBoundingClientRect();
	const wasHidden = highlight.hidden;
	const alreadySelected = highlight.classList.contains("hover--selected");
	highlight.hidden = false;
	if (selecting) {
		// Ease from flush bounds to a constant outset once per selection.
		if (wasHidden || !alreadySelected) {
			highlight.classList.remove("hover--selected");
			positionBox(highlight, rect, 0);
			requestAnimationFrame(() => {
				if (!session.draft) return;
				highlight.classList.add("hover--selected");
				positionBox(highlight, target.getBoundingClientRect(), SELECTED_OUTSET_PX);
			});
		} else {
			positionBox(highlight, rect, SELECTED_OUTSET_PX);
		}
	} else {
		highlight.classList.remove("hover--selected");
		positionBox(highlight, rect, 0);
	}
}

function renderMarkers(): void {
	const root = ensureOverlay();
	const markers = root.querySelector<HTMLElement>(".markers")!;
	markers.innerHTML = "";
	const stacks = new Map<string, number>();
	for (const annotation of session.annotations) {
		const element = resolveTarget(annotation.target);
		if (!element) continue;
		const rect = element.getBoundingClientRect();
		const key = `${Math.round(rect.left)}:${Math.round(rect.top)}`;
		const offset = stacks.get(key) ?? 0;
		stacks.set(key, offset + 1);
		const marker = document.createElement("button");
		marker.className = "marker chrome";
		marker.type = "button";
		marker.textContent = String(annotation.number);
		marker.title = `Edit ${annotation.kind} ${annotation.number}`;
		marker.style.left = `${Math.max(4, rect.right - 9 + offset * 15)}px`;
		marker.style.top = `${Math.max(4, rect.top - 9)}px`;
		marker.addEventListener("click", (event) => {
			event.stopPropagation();
			if (!session.draft) openComposer(element, annotation);
		});
		markers.appendChild(marker);
	}
}

function renderComposer(): void {
	const mount = ensureOverlay().querySelector<HTMLElement>(".composer-mount")!;
	const draft = session.draft;
	if (!draft) { mount.innerHTML = ""; return; }
	const target = selectedElement ?? resolveTarget(draft.target);
	mount.innerHTML = `
		<form class="composer chrome composer--${draft.kind}" aria-label="Annotate selection">
			<div class="composer-input-row">
				${ADJUST_MODE_ENABLED ? `<button type="button" data-action="adjust" class="adjust-button${draft.kind === "adjustment" ? " adjust-button--active" : ""}" aria-label="${draft.kind === "adjustment" ? "Return to comment" : "Adjust element"}" title="${draft.kind === "adjustment" ? "Return to comment" : "Adjust element"}">${adjustIcon()}</button>` : ""}
				<textarea class="composer-note" rows="1" aria-label="${draft.kind === "adjustment" ? "Adjustment note" : "Comment"}" placeholder="${draft.kind === "adjustment" ? "Describe these changes…" : "Add a comment..."}"></textarea>
			</div>
			${ADJUST_MODE_ENABLED && draft.kind === "adjustment" && target ? adjustmentPanel(target) : ""}
		</form>`;
	const form = mount.querySelector<HTMLFormElement>("form")!;
	const textarea = form.querySelector<HTMLTextAreaElement>(".composer-note")!;
	textarea.value = draft.body;
	if (!composerAnchorRect && target) composerAnchorRect = copyRect(target.getBoundingClientRect());
	const resize = () => composerAnchorRect && resizeAndPositionComposer(form, textarea, composerAnchorRect);
	textarea.addEventListener("input", () => {
		draft.body = textarea.value;
		if (draft.kind === "comment") {
			form.classList.toggle("composer--multiline", textarea.value.includes("\n") || textarea.scrollHeight > COMMENT_TEXTAREA_MIN_HEIGHT + 4);
		}
		resize();
		emitState();
	});
	textarea.addEventListener("keydown", (event) => {
		event.stopPropagation();
		if (event.key === "Enter" && !event.shiftKey && !event.metaKey && !event.ctrlKey) { event.preventDefault(); saveDraft(); }
		else if (event.key === "Escape") { event.preventDefault(); closeComposer(); }
		else if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) { event.preventDefault(); void submitBatch(); }
	});
	form.addEventListener("submit", (event) => { event.preventDefault(); saveDraft(); });
	form.addEventListener("keydown", (event) => {
		if (event.target === textarea) return;
		event.stopPropagation();
		if (event.key === "Escape") {
			event.preventDefault();
			closeComposer();
		}
	});
	form.querySelector<HTMLElement>('[data-action="adjust"]')?.addEventListener("click", () => {
		if (draft.kind === "adjustment") {
			if (target) restoreAdjustments(target, draft.adjustments);
			draft.adjustments = [];
			draft.kind = "comment";
			adjustmentLocks = emptyAdjustmentLocks();
		} else {
			draft.kind = "adjustment";
		}
		emitState();
		renderComposer();
	});
	form.querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>("[data-property]").forEach((input) => {
		input.addEventListener("input", () => {
			if (!target) return;
			const property = input.dataset.property as BrowserAdjustmentProperty;
			const value = adjustmentInputValue(input, property);
			updateAdjustment(target, property, value);
			if (COLOR_PROPERTIES.has(property)) {
				const valueLabel = input.closest(".field")?.querySelector<HTMLElement>(".color-value");
				if (valueLabel) valueLabel.textContent = value.toUpperCase();
			}
		});
		input.addEventListener("change", () => {
			const property = input.dataset.property as BrowserAdjustmentProperty;
			if (PIXEL_PROPERTIES.has(property)) input.value = pixelControlValue(adjustmentInputValue(input, property));
		});
	});
	form.querySelectorAll<HTMLButtonElement>("[data-reset]").forEach((button) => button.addEventListener("click", () => {
		if (target) resetAdjustment(target, button.dataset.reset as BrowserAdjustmentProperty);
	}));
	form.querySelectorAll<HTMLButtonElement>("[data-lock]").forEach((button) => button.addEventListener("click", () => {
		const lock = button.dataset.lock as AdjustmentLockName;
		adjustmentLocks[lock] = !adjustmentLocks[lock];
		button.classList.toggle("link-button--active", adjustmentLocks[lock]);
		button.setAttribute("aria-pressed", String(adjustmentLocks[lock]));
		if (adjustmentLocks[lock] && target) synchronizeLockedValues(target, lock);
		else if (lock === "ratio") adjustmentLocks.ratioValue = 0;
	}));
	resize();
}

function icon(name: "camera" | "eye" | "trash" | "close" | "arrow" | "check" | "sliders" | "sliders-horizontal" | "tune" | "sparkles" | "settings" | "grip" | "reset" | "link"): string {
	const paths = {
		camera: '<path d="M14.5 5 13 3h-4L7.5 5H4a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2z"/><circle cx="11" cy="11" r="3.5"/>',
		eye: '<path d="M2 12s3.5-6 10-6 10 6 10 6-3.5 6-10 6S2 12 2 12z"/><circle cx="12" cy="12" r="2.5"/>',
		trash: '<path d="M3 6h18M8 6V4h8v2m-9 0 1 14h8l1-14M10 10v6m4-6v6"/>',
		close: '<path d="m6 6 12 12M18 6 6 18"/>',
		arrow: '<path d="M12 19V5m-7 7 7-7 7 7"/>',
		check: '<path d="m5 12 4 4 10-10"/>',
		sliders: '<path d="M4 21v-7m0-4V3m8 18v-9m0-4V3m8 18v-5m0-4V3M1 14h6m2-6h6m2 8h6"/>',
		"sliders-horizontal": '<path d="M21 4H14M10 4H3M21 12h-9M8 12H3M21 20h-5M12 20H3M14 2v4M8 10v4M16 18v4"/>',
		tune: '<path d="M12 22a1 1 0 0 1 0-20 10 10 0 0 1 10 10 1 1 0 0 1-2 0 7 7 0 0 0-7-7 1 1 0 0 1 0-2 7 7 0 0 0 7 7 1 1 0 0 1 0 2 10 10 0 0 1-10-10z"/>',
		sparkles: '<path d="M15 4 16 8l4 1-3 3 1 4-4-2-4 2 1-4-3-3 4-1 1-4m-9 3 1 2 2 1-2 1-1 2-1-2-2-1 2-1 1-2m18 12 1 2 2 1-2 1-1 2-1-2-2-1 2-1 1-2"/>',
		settings: '<path d="M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41"/>',
		grip: '<circle cx="9" cy="7" r="1"/><circle cx="15" cy="7" r="1"/><circle cx="9" cy="12" r="1"/><circle cx="15" cy="12" r="1"/><circle cx="9" cy="17" r="1"/><circle cx="15" cy="17" r="1"/>',
		reset: '<path d="M3 12a9 9 0 1 0 3-6.7L3 8m0-5v5h5"/>',
		link: '<path d="M10 13a5 5 0 0 0 7.54.54l2-2a5 5 0 0 0-7.07-7.07l-1.15 1.15M14 11a5 5 0 0 0-7.54-.54l-2 2a5 5 0 0 0 7.07 7.07l1.14-1.14"/>',
	};
	return `<svg viewBox="0 0 24 24" aria-hidden="true">${paths[name]}</svg>`;
}

function adjustIcon(): string {
	return icon(ADJUST_ICON);
}

function adjustmentPanel(element: Element): string {
	const draft = session.draft;
	if (!draft) return "";
	const field = (property: BrowserAdjustmentProperty) => {
		const definition = ADJUSTMENTS.find((item) => item.property === property);
		return definition ? adjustmentField(element, definition, draft.adjustments) : "";
	};
	const linkButton = (lock: AdjustmentLockName, label: string) => `<button type="button" class="link-button${adjustmentLocks[lock] ? " link-button--active" : ""}" data-lock="${lock}" aria-label="${label}" title="${label}" aria-pressed="${adjustmentLocks[lock]}">${icon("link")}</button>`;
	const display = getComputedStyle(element).display;
	const hasEditableText = Boolean(editableTextNode(element));
	const layout = display === "flex" || display === "inline-flex"
		? `<div class="adjustment-group">${field("flexDirection")}${field("alignItems")}${field("justifyContent")}${field("columnGap")}${field("rowGap")}</div>`
		: "";
	return `<div class="element-header"><strong>${escapeAttribute(element.tagName.toLowerCase())}</strong><span>${icon("grip")}</span></div>
		<div class="adjustment-scroll">
			${hasEditableText ? `<div class="adjustment-group adjustment-group--content">${field("textContent")}</div>` : ""}
			<div class="adjustment-group">${hasEditableText ? field("color") : ""}${field("backgroundColor")}${field("opacity")}</div>
			${hasEditableText ? `<div class="adjustment-group">${field("fontFamily")}${field("fontSize")}${field("fontWeight")}</div>` : ""}
			<div class="adjustment-group">${field("borderRadius")}${field("borderColor")}${field("borderWidth")}</div>
			<div class="adjustment-group linked-group">${field("width")}${linkButton("ratio", "Lock width and height ratio")}${field("height")}</div>
			<details class="spacing-section" open><summary>Padding</summary><div class="spacing-fields"><div class="linked-group">${field("paddingTop")}${linkButton("paddingVertical", "Link top and bottom padding")}${field("paddingBottom")}</div><div class="linked-group">${field("paddingLeft")}${linkButton("paddingHorizontal", "Link left and right padding")}${field("paddingRight")}</div></div></details>
			<details class="spacing-section"><summary>Margin</summary><div class="spacing-fields"><div class="linked-group">${field("marginTop")}${linkButton("marginVertical", "Link top and bottom margin")}${field("marginBottom")}</div><div class="linked-group">${field("marginLeft")}${linkButton("marginHorizontal", "Link left and right margin")}${field("marginRight")}</div></div></details>
			${layout}
	</div>`;
}

function adjustmentField(element: Element, definition: AdjustmentDefinition, adjustments: BrowserStyleAdjustment[]): string {
	const adjustment = adjustments.find((item) => item.property === definition.property);
	const value = adjustment?.value ?? baselineValue(element, definition.property);
	const escapedValue = escapeAttribute(value);
	let field: string;
	if (definition.property === "textContent") {
		field = `<textarea class="property-textarea" data-property="textContent" rows="2" spellcheck="true">${escapeHtml(value)}</textarea>`;
	} else if (COLOR_PROPERTIES.has(definition.property)) {
		const color = colorInputValue(value);
		field = `<input class="color-picker" type="color" data-property="${definition.property}" value="${color}" aria-label="Choose ${escapeAttribute(definition.label.toLowerCase())}"><span class="color-value">${color.toUpperCase()}</span>`;
	} else if (definition.options) {
		const options = Array.from(new Set([value, ...definition.options])).filter(Boolean);
		field = `<select data-property="${definition.property}">${options.map((option) => `<option value="${escapeAttribute(option)}" ${option === value ? "selected" : ""}>${escapeHtml(option)}</option>`).join("")}</select>`;
	} else if (PIXEL_PROPERTIES.has(definition.property)) {
		field = `<input data-property="${definition.property}" data-unit="px" value="${escapeAttribute(pixelControlValue(value))}" inputmode="decimal" spellcheck="false"><span class="unit">px</span>`;
	} else {
		field = `<input data-property="${definition.property}" value="${escapedValue}" spellcheck="false">`;
	}
	return `<label class="adjustment-row${definition.property === "textContent" ? " adjustment-row--content" : ""}"><span>${definition.label}</span><span class="field${COLOR_PROPERTIES.has(definition.property) ? " field--color" : ""}">${field}<button type="button" class="reset-button${adjustment ? " reset-button--changed" : ""}" data-reset="${definition.property}" title="Reset ${definition.label}" ${adjustment ? "" : "disabled"}>${icon("reset")}</button></span></label>`;
}

function adjustmentInputValue(input: HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement, property: BrowserAdjustmentProperty): string {
	const value = input.value.trim();
	if (!PIXEL_PROPERTIES.has(property) || !value) return value;
	return /^-?(?:\d+\.?\d*|\.\d+)$/.test(value) ? `${String(Number(value))}px` : value;
}

function pixelControlValue(value: string): string {
	const match = value.trim().match(/^(-?(?:\d+\.?\d*|\.\d+))px$/i);
	return match?.[1] ?? value;
}

function colorInputValue(value: string): string {
	const normalized = value.trim().toLowerCase();
	if (/^#[\da-f]{6}$/.test(normalized)) return normalized;
	if (/^#[\da-f]{3}$/.test(normalized))
		return `#${normalized.slice(1).split("").map((part) => `${part}${part}`).join("")}`;
	const rgb = normalized.match(/^rgba?\(\s*([\d.]+)(?:\s*,\s*|\s+)([\d.]+)(?:\s*,\s*|\s+)([\d.]+)/);
	if (!rgb) return "#000000";
	return `#${rgb.slice(1, 4).map((part) => Math.max(0, Math.min(255, Math.round(Number(part)))).toString(16).padStart(2, "0")).join("")}`;
}

const NON_EDITABLE_TEXT_TAGS = new Set(["IMG", "SVG", "PICTURE", "VIDEO", "CANVAS", "INPUT", "TEXTAREA", "SELECT", "OPTION"]);

function editableTextNode(element: Element): Text | null {
	if (NON_EDITABLE_TEXT_TAGS.has(element.tagName)) return null;
	const known = editableTextTargets.get(element);
	if (known?.isConnected) return known;
	const direct = Array.from(element.childNodes).filter(
		(node): node is Text => node.nodeType === Node.TEXT_NODE && Boolean(node.nodeValue?.trim()),
	);
	if (direct.length === 1) {
		editableTextTargets.set(element, direct[0]);
		return direct[0];
	}
	if (direct.length > 1) return null;
	const elementText = compactVisibleText(element.textContent ?? "");
	if (!elementText) return null;
	const candidates = Array.from(element.querySelectorAll("*")).flatMap((candidate) => {
		if (NON_EDITABLE_TEXT_TAGS.has(candidate.tagName)) return [];
		const nodes = Array.from(candidate.childNodes).filter(
			(node): node is Text => node.nodeType === Node.TEXT_NODE && Boolean(node.nodeValue?.trim()),
		);
		return nodes.length === 1 && compactVisibleText(candidate.textContent ?? "") === elementText ? nodes : [];
	});
	if (candidates.length !== 1) return null;
	editableTextTargets.set(element, candidates[0]);
	return candidates[0];
}

function compactVisibleText(value: string): string {
	return value.replace(/\s+/g, " ").trim();
}

function adjustmentStyleTarget(element: Element, property: BrowserAdjustmentProperty): Element {
	if (!TEXT_STYLE_PROPERTIES.has(property)) return element;
	return editableTextNode(element)?.parentElement ?? element;
}

function updateAdjustment(element: Element, property: BrowserAdjustmentProperty, value: string, link = true): void {
	const draft = session.draft;
	if (!draft) return;
	if (property !== "textContent" && value && globalThis.CSS?.supports && !globalThis.CSS.supports(kebabCase(property), value)) return;
	const previousValue = baselineValue(element, property);
	const index = draft.adjustments.findIndex((item) => item.property === property);
	if ((property !== "textContent" && !value) || value === previousValue) {
		if (index >= 0) draft.adjustments.splice(index, 1);
		setElementValue(element, property, previousValue);
	} else if (index >= 0) draft.adjustments[index] = { property, previousValue, value };
	else draft.adjustments.push({ property, previousValue, value });
	if ((property === "textContent" || value) && value !== previousValue) setElementValue(element, property, value);
	if (link) applyLocks(element, property, value);
	emitState();
	renderMarkers();
}

function resetAdjustment(element: Element, property: BrowserAdjustmentProperty): void {
	const draft = session.draft;
	if (!draft) return;
	const existing = draft.adjustments.find((item) => item.property === property);
	if (existing) setElementValue(element, property, existing.previousValue);
	draft.adjustments = draft.adjustments.filter((item) => item.property !== property);
	emitState();
	renderComposer();
	renderMarkers();
}

function applyLocks(element: Element, property: BrowserAdjustmentProperty, value: string): void {
	if (!value) return;
	if (adjustmentLocks.marginHorizontal) {
		if (property === "marginLeft") updateLinkedAdjustment(element, "marginRight", value);
		if (property === "marginRight") updateLinkedAdjustment(element, "marginLeft", value);
	}
	if (adjustmentLocks.paddingHorizontal) {
		if (property === "paddingLeft") updateLinkedAdjustment(element, "paddingRight", value);
		if (property === "paddingRight") updateLinkedAdjustment(element, "paddingLeft", value);
	}
	if (adjustmentLocks.marginVertical) {
		if (property === "marginTop") updateLinkedAdjustment(element, "marginBottom", value);
		if (property === "marginBottom") updateLinkedAdjustment(element, "marginTop", value);
	}
	if (adjustmentLocks.paddingVertical) {
		if (property === "paddingTop") updateLinkedAdjustment(element, "paddingBottom", value);
		if (property === "paddingBottom") updateLinkedAdjustment(element, "paddingTop", value);
	}
	if (adjustmentLocks.ratio && adjustmentLocks.ratioValue > 0 && (property === "width" || property === "height")) {
		const dimension = parseCssDimension(value);
		if (!dimension) return;
		const linkedProperty = property === "width" ? "height" : "width";
		const linkedAmount = property === "width"
			? dimension.amount / adjustmentLocks.ratioValue
			: dimension.amount * adjustmentLocks.ratioValue;
		updateLinkedAdjustment(element, linkedProperty, `${formatCssNumber(linkedAmount)}${dimension.unit}`);
	}
}

function synchronizeLockedValues(element: Element, lock: AdjustmentLockName): void {
	if (lock === "ratio") {
		const rect = element.getBoundingClientRect();
		adjustmentLocks.ratioValue = rect.width && rect.height ? rect.width / rect.height : 0;
	} else if (lock === "marginHorizontal") {
		const style = getComputedStyle(element);
		updateLinkedAdjustment(element, "marginRight", style.marginLeft);
	} else if (lock === "paddingHorizontal") {
		const style = getComputedStyle(element);
		updateLinkedAdjustment(element, "paddingRight", style.paddingLeft);
	} else if (lock === "marginVertical") {
		const style = getComputedStyle(element);
		updateLinkedAdjustment(element, "marginBottom", style.marginTop);
	} else if (lock === "paddingVertical") {
		const style = getComputedStyle(element);
		updateLinkedAdjustment(element, "paddingBottom", style.paddingTop);
	}
}

function updateLinkedAdjustment(element: Element, property: BrowserAdjustmentProperty, value: string): void {
	updateAdjustment(element, property, value, false);
	const control = shadow?.querySelector<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>(`[data-property="${property}"]`);
	if (control) control.value = PIXEL_PROPERTIES.has(property) ? pixelControlValue(value) : value;
}

function parseCssDimension(value: string): { amount: number; unit: string } | null {
	const match = value.trim().match(/^(-?(?:\d+\.?\d*|\.\d+))\s*(px|rem|em|%|vw|vh|vmin|vmax|ch|ex|cm|mm|in|pt|pc)?$/i);
	if (!match) return null;
	const amount = Number(match[1]);
	return Number.isFinite(amount) ? { amount, unit: match[2] ?? "px" } : null;
}

function formatCssNumber(value: number): string {
	return String(Math.round(value * 100) / 100);
}

function baselineValue(element: Element, property: BrowserAdjustmentProperty): string {
	let values = adjustmentBaseline.get(element);
	if (!values) { values = new Map(); adjustmentBaseline.set(element, values); }
	const known = values.get(property);
	if (known !== undefined) return known;
	const styleTarget = adjustmentStyleTarget(element, property);
	const rawValue = property === "textContent"
		? editableTextNode(element)?.nodeValue ?? ""
		: (getComputedStyle(styleTarget) as unknown as Record<string, string>)[property] ?? "";
	let value = rawValue;
	if (property === "alignItems" || property === "justifyContent") value = rawValue.replace(/^flex-/, "");
	if (property === "alignItems" && value === "normal") value = "stretch";
	if (property === "justifyContent" && value === "normal") value = "start";
	values.set(property, value);
	return value;
}

function setElementValue(element: Element, property: BrowserAdjustmentProperty, value: string): void {
	if (property === "textContent") {
		const textNode = editableTextNode(element);
		if (textNode) textNode.nodeValue = value;
		return;
	}
	const styleTarget = adjustmentStyleTarget(element, property) as HTMLElement;
	styleTarget.style.setProperty(kebabCase(property), value, "important");
}

function applyAllAdjustments(): void {
	if (showingOriginal) return;
	for (const annotation of session.annotations) {
		const element = resolveTarget(annotation.target);
		if (!element) continue;
		for (const adjustment of annotation.adjustments) {
			baselineValue(element, adjustment.property);
			setElementValue(element, adjustment.property, adjustment.value);
		}
	}
}

function restoreAdjustments(element: Element, adjustments: BrowserStyleAdjustment[]): void {
	for (const adjustment of adjustments) setElementValue(element, adjustment.property, adjustment.previousValue);
}

function setOriginalPreview(next: boolean): void {
	if (showingOriginal === next) return;
	showingOriginal = next;
	if (next) {
		for (const annotation of session.annotations) {
			const element = resolveTarget(annotation.target);
			if (element) restoreAdjustments(element, annotation.adjustments);
		}
		if (selectedElement && session.draft) restoreAdjustments(selectedElement, session.draft.adjustments);
		host?.setAttribute("data-original", "true");
		return;
	}
	host?.removeAttribute("data-original");
	applyAllAdjustments();
	if (selectedElement && session.draft)
		for (const adjustment of session.draft.adjustments)
			setElementValue(selectedElement, adjustment.property, adjustment.value);
}

function resolveTarget(target: BrowserAnnotationTarget): Element | null {
	const context = target.context;
	const exact = uniqueQuery(context.selector);
	if (exact) return exact;
	if (context.id) {
		const byId = document.getElementById(context.id);
		if (byId?.tagName.toLowerCase() === context.tag) return byId;
	}
	if (context.testId) {
		const byTestId = uniqueQuery(`[data-testid="${cssEscape(context.testId)}"]`);
		if (byTestId?.tagName.toLowerCase() === context.tag) return byTestId;
	}
	const candidates = Array.from(document.querySelectorAll(context.tag)).filter((candidate) => {
		if (context.role && candidate.getAttribute("role") !== context.role) return false;
		if (context.ariaLabel && createBrowserAnnotationContext(candidate).ariaLabel !== context.ariaLabel) return false;
		if (context.visibleText && (candidate.textContent ?? "").replace(/\s+/g, " ").trim() !== context.visibleText) return false;
		return true;
	});
	return candidates.length === 1 ? candidates[0] : null;
}

function uniqueQuery(selector: string): Element | null {
	try { const matches = document.querySelectorAll(selector); return matches.length === 1 ? matches[0] : null; } catch { return null; }
}

function refreshPositions(): void {
	if (!enabled) return;
	renderHover();
	renderMarkers();
	if (!session.draft) return;
	const form = shadow?.querySelector<HTMLFormElement>(".composer");
	const textarea = form?.querySelector<HTMLTextAreaElement>(".composer-note");
	const target = selectedElement ?? resolveTarget(session.draft.target);
	if (!composerAnchorRect && target) composerAnchorRect = copyRect(target.getBoundingClientRect());
	if (form && textarea && composerAnchorRect) resizeAndPositionComposer(form, textarea, composerAnchorRect);
}

function copyRect(rect: AnnotationRectLike): AnnotationRectLike {
	return { left: rect.left, top: rect.top, bottom: rect.bottom };
}

function positionBox(box: HTMLElement, rect: DOMRect, outset = 0): void {
	box.style.left = `${Math.max(0, rect.left - outset)}px`;
	box.style.top = `${Math.max(0, rect.top - outset)}px`;
	box.style.width = `${Math.max(0, rect.width + outset * 2)}px`;
	box.style.height = `${Math.max(0, rect.height + outset * 2)}px`;
}

function resizeAndPositionComposer(form: HTMLFormElement, textarea: HTMLTextAreaElement, rect: AnnotationRectLike): void {
	const viewportWidth = Math.min(window.innerWidth, document.documentElement.clientWidth || window.innerWidth);
	const viewportHeight = Math.min(window.innerHeight, document.documentElement.clientHeight || window.innerHeight);
	const adjusting = form.classList.contains("composer--adjustment");
	const width = Math.max(0, Math.min(adjusting ? ADJUSTMENT_WIDTH : COMMENT_WIDTH, viewportWidth - PROMPT_GUTTER * 2));
	form.style.width = `${width}px`;
	if (adjusting) {
		form.style.maxHeight = `${Math.max(180, Math.min(ADJUSTMENT_MAX_HEIGHT, viewportHeight - PROMPT_GUTTER * 2))}px`;
		textarea.style.height = "0px";
		textarea.style.height = `${Math.min(72, Math.max(COMMENT_TEXTAREA_MIN_HEIGHT, textarea.scrollHeight))}px`;
		textarea.style.overflowY = "hidden";
	} else {
		form.style.maxHeight = "none";
		textarea.style.height = "0px";
		const spaceAbove = rect.top - PROMPT_GAP - PROMPT_GUTTER;
		const spaceBelow = viewportHeight - PROMPT_GUTTER - rect.bottom - PROMPT_GAP;
		const availableHeight = Math.max(spaceAbove, spaceBelow);
		const maxComposerHeight = Math.max(44, Math.min(COMMENT_MAX_HEIGHT, availableHeight));
		const maxTextareaHeight = Math.max(COMMENT_TEXTAREA_MIN_HEIGHT, maxComposerHeight - COMMENT_CHROME_VERTICAL);
		const naturalHeight = Math.max(COMMENT_TEXTAREA_MIN_HEIGHT, textarea.scrollHeight);
		textarea.style.height = `${Math.min(maxTextareaHeight, naturalHeight)}px`;
		textarea.style.overflowY = naturalHeight > maxTextareaHeight ? "auto" : "hidden";
	}
	const height = Math.min(viewportHeight - PROMPT_GUTTER * 2, Math.max(44, form.getBoundingClientRect().height));
	const position = promptPositionForRect(rect, { width: viewportWidth, height: viewportHeight, promptWidth: width, promptHeight: height, gutter: PROMPT_GUTTER, gap: PROMPT_GAP });
	form.style.left = `${position.left}px`;
	form.style.top = `${position.top}px`;
}

function decodeBase64Font(dataUri: string): ArrayBuffer {
	const binary = atob(dataUri.slice(dataUri.indexOf(",") + 1));
	const bytes = new Uint8Array(binary.length);
	for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
	return bytes.buffer;
}

function registerFonts(root: ShadowRoot): void {
	if (typeof FontFace === "undefined") return;
	const set = (root as ShadowRoot & { fonts?: FontFaceSet }).fonts ?? document.fonts;
	const sans = new FontFace("Geist Variable", decodeBase64Font(geistLatinWoff2), { weight: "100 900" });
	const mono = new FontFace("Geist Mono Variable", decodeBase64Font(geistMonoLatinWoff2), { weight: "100 900" });
	set.add(sans); set.add(mono); void sans.load(); void mono.load();
}

function ensureOverlay(): ShadowRoot {
	if (shadow && host?.isConnected) return shadow;
	host = document.createElement("div");
	host.setAttribute("data-ao-annotation-root", "");
	host.style.cssText = "position:fixed;inset:0;z-index:2147483647;pointer-events:none";
	(document.documentElement ?? document.body).appendChild(host);
	shadow = host.attachShadow({ mode: "open" });
	registerFonts(shadow);
	shadow.innerHTML = `<style>${overlayStyles()}</style><div class="hover" hidden></div><div class="markers"></div><div class="composer-mount"></div><div class="screenshot-notice chrome" hidden>Screenshot added to this batch</div>`;
	return shadow;
}

function overlayStyles(): string {
	const vars = {
		background: theme.background ?? "oklch(0.185 0.006 285.885)",
		foreground: theme.foreground ?? "oklch(0.985 0 0)",
		muted: theme.muted ?? "oklch(0.274 0.006 286.033)",
		mutedForeground: theme.mutedForeground ?? "oklch(0.705 0.015 286.067)",
		border: theme.border ?? "oklch(1 0 0 / 10%)",
		accent: theme.accent ?? "oklch(0.92 0.004 286.32)",
		accentForeground: theme.accentForeground ?? "oklch(0.21 0.006 285.885)",
		destructive: theme.destructive ?? "oklch(0.704 0.191 22.216)",
	};
	// Mirror renderer Button/Input chrome: 8px radius, 28px controls, equal 8px padding,
	// Geist sans everywhere except code-like CSS values (px / hex).
	return `
		:host{
			all:initial;
			--bg:${vars.background};
			--fg:${vars.foreground};
			--muted:${vars.muted};
			--muted-fg:${vars.mutedForeground};
			--border:${vars.border};
			--accent:${vars.accent};
			--accent-fg:${vars.accentForeground};
			--danger:${vars.destructive};
			--radius:${COMPOSER_RADIUS}px;
			--control:${COMPOSER_CONTROL}px;
			--pad:${COMPOSER_PAD}px;
			--gap:${COMPOSER_GAP}px;
			font-family:"Geist Variable",system-ui,sans-serif;
			color:var(--fg);
		}
		.hover{
			position:fixed;box-sizing:border-box;border:2px solid #4d8dff;border-radius:var(--radius);
			background:rgba(77,141,255,.10);pointer-events:none;
			transition:left 180ms ease,top 180ms ease,width 180ms ease,height 180ms ease;
		}
		.marker{
			position:fixed;width:20px;height:20px;border:2px solid var(--bg);border-radius:50%;
			background:#74b98a;color:#101512;padding:0;
			font:600 10px/1 "Geist Variable",system-ui,sans-serif;font-variant-numeric:tabular-nums;
			pointer-events:auto;box-shadow:0 2px 8px rgba(0,0,0,.28);cursor:pointer;
		}
		button{
			display:inline-flex;height:var(--control);align-items:center;justify-content:center;
			border:1px solid transparent;border-radius:var(--radius);background:var(--muted);color:var(--fg);
			padding:0 10px;font:400 12px/1 "Geist Variable",system-ui,sans-serif;cursor:pointer;
			transition:background-color 120ms ease,border-color 120ms ease,color 120ms ease,opacity 120ms ease;
		}
		button:hover{background:color-mix(in oklch,var(--muted) 88%,var(--fg))}
		button:disabled{opacity:.5;cursor:default}
		button:focus-visible{outline:none;box-shadow:inset 0 0 0 2px var(--accent)}
		.primary{background:var(--accent);color:var(--accent-fg)}
		.primary:hover{background:color-mix(in oklch,var(--accent) 88%,var(--fg))}
		.danger{color:var(--danger)}
		button svg{width:14px;height:14px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
		.composer{
			position:fixed;box-sizing:border-box;border:1px solid var(--border);
			background:var(--bg);color:var(--fg);
			box-shadow:0 8px 28px rgba(0,0,0,.32);pointer-events:auto;font-size:12px;overflow:hidden;
		}
		.composer--comment{
			border-radius:9999px;padding:8px 14px 8px 10px;
			transition:box-shadow 150ms ease,border-radius 150ms ease;
		}
		.composer--comment.composer--multiline{border-radius:${COMMENT_RADIUS}px;padding:10px 14px 10px 10px}
		.composer-input-row{display:flex;min-width:0;align-items:center;gap:8px}
		.adjust-button{
			width:var(--control);height:var(--control);flex:0 0 var(--control);
			border:0;border-radius:9999px;background:transparent;color:var(--muted-fg);padding:0;
		}
		.adjust-button:hover,.adjust-button--active{background:var(--muted);color:var(--fg)}
		.adjust-button svg{width:16px;height:16px}
		.composer-note{
			display:block;box-sizing:border-box;min-width:0;flex:1;resize:none;
			border:0;background:transparent;color:var(--fg);caret-color:var(--fg);
			padding:0;margin:0;font:400 13px/20px "Geist Variable",system-ui,sans-serif;outline:none;
			overflow-y:hidden;scrollbar-width:none;
		}
		.composer--comment .composer-note{height:20px;min-height:20px}
		.composer-note::-webkit-scrollbar,.property-textarea::-webkit-scrollbar,.adjustment-scroll::-webkit-scrollbar{display:none}
		.composer-note::placeholder{color:var(--muted-fg)}
		textarea:focus,input:focus,select:focus{outline:none}
		.composer--adjustment{
			display:flex;flex-direction:column;padding:0;background:var(--bg);border-radius:var(--radius);
		}
		.composer--adjustment .composer-input-row{flex:0 0 auto;padding:var(--pad);background:var(--muted)}
		.composer--adjustment .composer-note{max-height:52px;padding:0}
		.element-header{
			display:flex;flex:0 0 auto;align-items:center;justify-content:space-between;
			border-top:1px solid var(--border);border-bottom:1px solid var(--border);
			padding:var(--pad) 12px;font-size:12px;
		}
		.element-header strong{font-weight:600}
		.element-header span{color:var(--muted-fg)}
		.element-header svg{width:14px;height:14px}
		.adjustment-scroll{min-height:0;flex:1;overflow-y:auto;scrollbar-width:none}
		.adjustment-group{
			position:relative;display:flex;flex-direction:column;gap:var(--gap);
			border-bottom:1px solid var(--border);padding:var(--pad) 12px;
		}
		.adjustment-row{
			display:grid;grid-template-columns:88px minmax(0,1fr);align-items:center;gap:var(--gap);
			min-height:var(--control);color:var(--muted-fg);font-size:12px;
		}
		.adjustment-row--content{align-items:start}
		.adjustment-row--content>span:first-child{padding-top:8px}
		.field{position:relative;display:flex;min-width:0;align-items:center}
		.field input,.field select,.property-textarea{
			box-sizing:border-box;min-width:0;width:100%;height:var(--control);
			border:1px solid transparent;border-radius:var(--radius);
			background:color-mix(in oklch,var(--muted) 70%,transparent);color:var(--fg);
			padding:0 30px 0 8px;font:400 12px/17px "Geist Variable",system-ui,sans-serif;
		}
		.field input[data-unit]{font:400 12px/17px "Geist Mono Variable",ui-monospace,monospace;font-variant-numeric:tabular-nums}
		.property-textarea{
			height:46px;resize:none;padding:6px 28px 6px 8px;scrollbar-width:none;
		}
		.field select{padding-right:42px}
		.field input[data-unit]{padding-right:46px}
		.unit{
			position:absolute;right:30px;color:var(--muted-fg);
			font:400 10px/1 "Geist Mono Variable",ui-monospace,monospace;font-variant-numeric:tabular-nums;pointer-events:none;
		}
		.field--color{gap:8px;padding-right:28px}
		.field input.color-picker{
			width:28px;height:24px;flex:0 0 28px;border:0;border-radius:6px;
			background:transparent;padding:0;overflow:hidden;cursor:pointer;
		}
		.color-picker::-webkit-color-swatch-wrapper{padding:0}
		.color-picker::-webkit-color-swatch{border:1px solid var(--border);border-radius:6px}
		.color-value{
			min-width:0;overflow:hidden;color:var(--fg);
			font:400 12px/1 "Geist Mono Variable",ui-monospace,monospace;font-variant-numeric:tabular-nums;
			text-overflow:ellipsis;white-space:nowrap;
		}
		.reset-button{
			position:absolute;right:1px;width:var(--control);height:var(--control);
			border:0;border-radius:var(--radius);background:transparent;color:var(--muted-fg);padding:0;
		}
		.reset-button:hover{background:transparent;color:var(--fg)}
		.field:has(select) .reset-button{right:20px}
		.reset-button--changed{color:var(--fg)}
		.reset-button svg{width:12px;height:12px}
		.linked-group{position:relative}
		.link-button{
			position:absolute;z-index:2;left:87px;top:50%;width:20px;height:20px;
			transform:translate(-50%,-50%);border:1px solid var(--border);border-radius:6px;
			background:var(--bg);color:var(--muted-fg);padding:0;box-shadow:0 1px 3px rgba(0,0,0,.2);
		}
		.link-button:hover{background:var(--bg);color:var(--fg)}
		.link-button svg{width:11px;height:11px}
		.link-button--active{border-color:#4d8dff;background:color-mix(in oklch,#4d8dff 22%,var(--bg));color:#78a8ff}
		.link-button--active:hover{border-color:#4d8dff;background:color-mix(in oklch,#4d8dff 22%,var(--bg));color:#78a8ff}
		.spacing-section{border-bottom:1px solid var(--border);padding:0 12px}
		.spacing-section summary{
			cursor:pointer;color:var(--muted-fg);font-size:12px;font-weight:500;
			padding:var(--pad) 0;list-style:none;
		}
		.spacing-section summary::-webkit-details-marker{display:none}
		.spacing-section summary::after{
			content:"›";float:right;font-size:16px;line-height:13px;
			transform:rotate(90deg);transition:transform 120ms ease;
		}
		.spacing-section[open] summary::after{transform:rotate(-90deg)}
		.spacing-fields{display:flex;flex-direction:column;gap:var(--gap);padding:0 0 var(--pad)}
		.spacing-fields .linked-group{display:flex;flex-direction:column;gap:var(--gap)}
		.screenshot-notice{
			position:fixed;left:50%;top:55px;transform:translateX(-50%);
			border:1px solid var(--border);border-radius:var(--radius);background:var(--bg);color:var(--fg);
			padding:var(--pad) 12px;box-shadow:0 8px 22px rgba(0,0,0,.28);
			font:400 12px/1 "Geist Variable",system-ui,sans-serif;pointer-events:none;
		}
		:host([data-original]) .hover,:host([data-original]) .marker,:host([data-original]) .composer{visibility:hidden}
		@media(prefers-reduced-motion:reduce){*{transition:none!important}}
	`;
}

function cleanupOverlay(): void { host?.remove(); host = null; shadow = null; }
function isOverlayEvent(event: Event): boolean { return Boolean(host && event.composedPath().includes(host)); }
function waitForPaint(): Promise<void> { return new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))); }
function nextAnnotationNumber(): number { return session.annotations.reduce((max, item) => Math.max(max, item.number), 0) + 1; }
function localId(prefix: string): string { nextLocalId += 1; return `${prefix}-${Date.now().toString(36)}-${nextLocalId.toString(36)}`; }
function samePage(left: string, right: string): boolean { try { const a = new URL(left); const b = new URL(right); a.hash = ""; b.hash = ""; return a.href === b.href; } catch { return left.split("#")[0] === right.split("#")[0]; } }
function kebabCase(value: string): string { return value.replace(/[A-Z]/g, (letter) => `-${letter.toLowerCase()}`); }
function cssEscape(value: string): string { return globalThis.CSS?.escape ? globalThis.CSS.escape(value) : value.replace(/[^a-zA-Z0-9_-]/g, "\\$&"); }
function escapeAttribute(value: string): string { return value.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;"); }
function escapeHtml(value: string): string { return value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;"); }
