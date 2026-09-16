import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createBrowserAnnotationSession, type BrowserAnnotationSession } from "./shared/browser-annotations";

const electronMocks = vi.hoisted(() => {
	const listeners = new Map<string, (...args: unknown[]) => void>();
	return {
		listeners,
		on: vi.fn((channel: string, listener: (...args: unknown[]) => void) => listeners.set(channel, listener)),
		send: vi.fn(),
		invoke: vi.fn().mockResolvedValue(undefined),
	};
});

vi.mock("electron", () => ({
	ipcRenderer: {
		on: electronMocks.on,
		send: electronMocks.send,
		invoke: electronMocks.invoke,
	},
}));

const fontMocks = vi.hoisted(() => ({ add: vi.fn() }));
class MockFontFace {
	constructor(_family: string) {}
	load(): Promise<MockFontFace> { return Promise.resolve(this); }
}
vi.stubGlobal("FontFace", MockFontFace);
Object.defineProperty(document, "fonts", { configurable: true, value: { add: fontMocks.add } });

await import("./annotate-preload");

type Bounds = { left: number; top: number; width: number; height: number };

function setMode(enabled: boolean, session?: BrowserAnnotationSession): void {
	const listener = electronMocks.listeners.get("browser:annotation:setMode");
	if (!listener) throw new Error("annotation mode listener was not registered");
	listener({}, { enabled, ...(session ? { session } : {}) });
}

function setElementBounds<T extends Element>(element: T, bounds: Bounds): T {
	Object.defineProperty(element, "getBoundingClientRect", {
		configurable: true,
		value: () => ({
			x: bounds.left,
			y: bounds.top,
			left: bounds.left,
			top: bounds.top,
			right: bounds.left + bounds.width,
			bottom: bounds.top + bounds.height,
			width: bounds.width,
			height: bounds.height,
			toJSON: () => ({}),
		}) as DOMRect,
	});
	return element;
}

function clickPage(element: Element): void {
	element.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
}

function overlayRoot(): ShadowRoot {
	const host = document.querySelector<HTMLDivElement>("[data-ao-annotation-root]");
	if (!host?.shadowRoot) throw new Error("annotation overlay was not rendered");
	return host.shadowRoot;
}

function openAdjust(element: Element): ShadowRoot {
	clickPage(element);
	const root = overlayRoot();
	root.querySelector<HTMLButtonElement>('[data-action="adjust"]')?.click();
	return root;
}

function latestSession(): BrowserAnnotationSession {
	const call = electronMocks.send.mock.calls.findLast(([channel]) => channel === "browser:annotation:state");
	if (!call) throw new Error("annotation state was not emitted");
	return call[1] as BrowserAnnotationSession;
}

describe("annotation adjustment preload", () => {
	beforeEach(() => {
		document.body.innerHTML = "";
		electronMocks.send.mockClear();
		electronMocks.invoke.mockClear();
		setMode(true, createBrowserAnnotationSession(window.location.href));
	});

	afterEach(() => {
		setMode(false);
		document.body.innerHTML = "";
	});

	it("opens a compact adjust panel with native color controls", () => {
		const button = setElementBounds(document.createElement("button"), { left: 20, top: 30, width: 140, height: 36 });
		button.id = "primary";
		button.textContent = "Continue";
		document.body.appendChild(button);

		const root = openAdjust(button);
		const form = root.querySelector<HTMLFormElement>(".composer--adjustment");
		const color = root.querySelector<HTMLInputElement>('[data-property="color"]');
		const styles = root.querySelector("style")?.textContent ?? "";

		expect(form?.style.width).toBe("316px");
		expect(form?.style.maxHeight).toBe("400px");
		expect(color).toHaveAttribute("type", "color");
		expect(styles).toContain(".color-picker::-webkit-color-swatch");
		// Unified chrome: modest radius, equal padding, sans by default; mono only for CSS values.
		expect(styles).toContain("--radius:8px");
		expect(styles).toContain("--pad:8px");
		expect(styles).not.toContain("border-radius:999px");
		expect(styles).not.toContain('font:700 10px "Geist Mono Variable"');
		expect(styles).toContain('font:600 10px/1 "Geist Variable"');
		expect(styles).toMatch(/\.field input,\.field select,\.property-textarea\{[^}]*Geist Variable/);
		expect(styles).toMatch(/\.field input\[data-unit\]\{[^}]*Geist Mono Variable/);
		expect(styles).toContain("button:focus-visible{outline:none;box-shadow:inset");
		expect(styles).toContain(".link-button--active:hover{");
	});

	it("applies a picked text color live to the element that paints nested text", () => {
		const button = setElementBounds(document.createElement("button"), { left: 20, top: 30, width: 140, height: 36 });
		button.id = "nested-label";
		const icon = document.createElementNS("http://www.w3.org/2000/svg", "svg");
		const label = document.createElement("span");
		label.textContent = "Continue";
		button.append(icon, label);
		document.body.appendChild(button);

		const root = openAdjust(button);
		const color = root.querySelector<HTMLInputElement>('[data-property="color"]')!;
		color.value = "#e34b63";
		color.dispatchEvent(new Event("input", { bubbles: true }));

		expect(label.style.getPropertyValue("color")).toBe("rgb(227, 75, 99)");
		expect(label.style.getPropertyPriority("color")).toBe("important");
		expect(root.querySelector(".color-value")?.textContent).toBe("#E34B63");
		expect(latestSession().draft?.adjustments).toContainEqual(expect.objectContaining({
			property: "color",
			value: "#e34b63",
		}));
	});

	it("edits only the direct text node and preserves nested markup", () => {
		const button = setElementBounds(document.createElement("button"), { left: 20, top: 30, width: 140, height: 36 });
		button.id = "safe-text";
		const icon = document.createElementNS("http://www.w3.org/2000/svg", "svg");
		const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
		icon.appendChild(path);
		const label = document.createElement("span");
		label.textContent = "Continue";
		button.append(icon, label);
		document.body.appendChild(button);

		const root = openAdjust(button);
		const text = root.querySelector<HTMLTextAreaElement>('[data-property="textContent"]')!;
		text.value = "Launch";
		text.dispatchEvent(new Event("input", { bubbles: true }));

		expect(label.textContent).toBe("Launch");
		expect(button.querySelector("svg path")).toBe(path);

		text.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
		expect(label.textContent).toBe("Continue");
		expect(button.querySelector("svg path")).toBe(path);
	});

	it("does not offer text editing for logos and non-text elements", () => {
		const image = setElementBounds(document.createElement("img"), { left: 20, top: 30, width: 80, height: 80 });
		image.id = "logo";
		document.body.appendChild(image);

		const root = openAdjust(image);

		expect(root.querySelector('[data-property="textContent"]')).toBeNull();
		expect(root.querySelector('[data-property="color"]')).toBeNull();
		expect(root.querySelector<HTMLInputElement>('[data-property="backgroundColor"]')).toHaveAttribute("type", "color");
	});

	it("keeps the inspector anchored while target dimensions change", () => {
		const bounds = { left: 620, top: 180, width: 160, height: 44 };
		const button = document.createElement("button");
		button.id = "resizable";
		button.textContent = "Resize me";
		Object.defineProperty(button, "getBoundingClientRect", {
			configurable: true,
			value: () => ({
				x: bounds.left,
				y: bounds.top,
				left: bounds.left,
				top: bounds.top,
				right: bounds.left + bounds.width,
				bottom: bounds.top + bounds.height,
				width: bounds.width,
				height: bounds.height,
				toJSON: () => ({}),
			}) as DOMRect,
		});
		document.body.appendChild(button);

		const root = openAdjust(button);
		const form = root.querySelector<HTMLFormElement>(".composer--adjustment")!;
		const width = root.querySelector<HTMLInputElement>('[data-property="width"]')!;
		const initialPosition = { left: form.style.left, top: form.style.top };

		bounds.left = 80;
		bounds.top = 500;
		width.value = "10";
		width.dispatchEvent(new Event("input", { bubbles: true }));
		width.value = "70";
		width.dispatchEvent(new Event("input", { bubbles: true }));

		expect(form.style.left).toBe(initialPosition.left);
		expect(form.style.top).toBe(initialPosition.top);
		expect(button.style.getPropertyValue("width")).toBe("70px");
	});

	it("normalizes leading zeroes before storing pixel adjustments", () => {
		const button = setElementBounds(document.createElement("button"), { left: 20, top: 30, width: 140, height: 36 });
		button.id = "normalized-size";
		button.textContent = "Resize me";
		document.body.appendChild(button);

		const root = openAdjust(button);
		const width = root.querySelector<HTMLInputElement>('[data-property="width"]')!;
		width.value = "089";
		width.dispatchEvent(new Event("input", { bubbles: true }));
		width.dispatchEvent(new Event("change", { bubbles: true }));

		expect(width.value).toBe("89");
		expect(button.style.getPropertyValue("width")).toBe("89px");
		expect(latestSession().draft?.adjustments).toContainEqual(expect.objectContaining({
			property: "width",
			value: "89px",
		}));
	});

	it("uses a pill comment composer aligned on one row without cancel or send buttons", () => {
		const button = setElementBounds(document.createElement("button"), { left: 20, top: 30, width: 140, height: 36 });
		button.id = "comment-row";
		button.textContent = "Continue";
		document.body.appendChild(button);

		clickPage(button);
		const root = overlayRoot();
		const form = root.querySelector<HTMLFormElement>(".composer--comment");
		const row = root.querySelector<HTMLElement>(".composer-input-row");
		const styles = root.querySelector("style")?.textContent ?? "";

		expect(form).not.toBeNull();
		expect(root.querySelector(".composer-actions")).toBeNull();
		expect(root.querySelector(".cancel-button")).toBeNull();
		expect(root.querySelector(".send-button")).toBeNull();
		expect(styles).toContain("border-radius:9999px");
		expect(styles).toContain(".composer-input-row{display:flex;min-width:0;align-items:center");
		expect(row?.querySelector(".adjust-button")).not.toBeNull();
		expect(row?.querySelector(".composer-note")).not.toBeNull();
	});

	it("discards an open comment when clicking outside the composer", () => {
		const button = setElementBounds(document.createElement("button"), { left: 20, top: 30, width: 140, height: 36 });
		button.id = "outside-dismiss";
		button.textContent = "Continue";
		document.body.appendChild(button);

		clickPage(button);
		expect(latestSession().draft).toBeDefined();

		document.body.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
		expect(latestSession().draft).toBeUndefined();
		expect(overlayRoot().querySelector(".composer")).toBeNull();
	});

	it("keeps the selection highlight visible with a 6px outset while annotating", async () => {
		const button = setElementBounds(document.createElement("button"), { left: 20, top: 30, width: 140, height: 36 });
		button.id = "selected-box";
		button.textContent = "Continue";
		document.body.appendChild(button);

		const styles = overlayRoot().querySelector("style")?.textContent ?? "";
		expect(styles).toContain("transition:left 180ms ease,top 180ms ease,width 180ms ease,height 180ms ease");
		expect(styles).not.toContain("transform:scale(1.1)");

		clickPage(button);
		const highlight = overlayRoot().querySelector<HTMLElement>(".hover");
		expect(highlight?.hidden).toBe(false);
		await vi.waitFor(() => {
			expect(highlight?.classList.contains("hover--selected")).toBe(true);
			expect(highlight?.style.left).toBe("14px");
			expect(highlight?.style.top).toBe("24px");
			expect(highlight?.style.width).toBe("152px");
			expect(highlight?.style.height).toBe("48px");
		});
	});

	it("clears open selection when re-entering annotation mode but keeps batch markers", () => {
		const button = setElementBounds(document.createElement("button"), { left: 20, top: 30, width: 140, height: 36 });
		button.id = "persist-batch";
		button.textContent = "Continue";
		document.body.appendChild(button);

		clickPage(button);
		const note = overlayRoot().querySelector<HTMLTextAreaElement>(".composer-note")!;
		note.value = "keep this in the batch";
		note.dispatchEvent(new Event("input", { bubbles: true }));
		note.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
		expect(latestSession().annotations).toHaveLength(1);
		expect(latestSession().draft).toBeUndefined();
		expect(overlayRoot().querySelectorAll(".marker")).toHaveLength(1);

		clickPage(button);
		expect(overlayRoot().querySelector(".composer")).not.toBeNull();
		expect(latestSession().draft).toBeDefined();

		// Main strips draft synchronously on disable, then re-sends the batch.
		const leaving = structuredClone(latestSession());
		delete leaving.draft;
		setMode(false, leaving);
		expect(document.querySelector("[data-ao-annotation-root]")).toBeNull();

		setMode(true, leaving);

		expect(overlayRoot().querySelector(".composer")).toBeNull();
		expect(latestSession().draft).toBeUndefined();
		expect(latestSession().annotations).toHaveLength(1);
		expect(overlayRoot().querySelectorAll(".marker")).toHaveLength(1);
	});
});
