import { describe, expect, it, vi } from "vitest";
import { buildMacAppMenuTemplate, buildWindowsAppMenuTemplate } from "./menu";

type MenuItem = ReturnType<typeof buildWindowsAppMenuTemplate>[number];
type SubmenuItem = NonNullable<Extract<MenuItem["submenu"], readonly unknown[]>>[number];

function viewSubmenu(): readonly SubmenuItem[] {
	const viewMenu = buildWindowsAppMenuTemplate().find((item) => item.label === "View");
	if (!viewMenu || !Array.isArray(viewMenu.submenu)) {
		throw new Error("View menu not found");
	}
	return viewMenu.submenu;
}

describe("buildWindowsAppMenuTemplate", () => {
	it("does not expose application zoom controls", () => {
		expect(viewSubmenu()).not.toContainEqual(
			expect.objectContaining({ role: expect.stringMatching(/^zoom|resetZoom$/) }),
		);
	});
});

describe("buildMacAppMenuTemplate", () => {
	function macViewSubmenu(onToggleDevTools = () => undefined): readonly SubmenuItem[] {
		const viewMenu = buildMacAppMenuTemplate(onToggleDevTools).find(
			(item) => item.label === "View",
		);
		if (!viewMenu || !Array.isArray(viewMenu.submenu)) {
			throw new Error("View menu not found");
		}
		return viewMenu.submenu;
	}

	it("uses a guarded click handler instead of Electron's crashing DevTools role", () => {
		const onToggleDevTools = vi.fn();
		const devtoolsItems = macViewSubmenu(onToggleDevTools).filter(
			(item) => item.label === "Toggle Developer Tools" || item.role === "toggleDevTools",
		);

		expect(devtoolsItems).toHaveLength(1);
		expect(devtoolsItems[0]).toMatchObject({
			accelerator: "Alt+Command+I",
			label: "Toggle Developer Tools",
		});
		expect(devtoolsItems[0].role).toBeUndefined();

		devtoolsItems[0].click?.(undefined as never, undefined as never, undefined as never);
		expect(onToggleDevTools).toHaveBeenCalledOnce();
	});

	it("preserves Electron's complete standard macOS menus", () => {
		const template = buildMacAppMenuTemplate(() => undefined);

		expect(template.map((item) => item.role)).toEqual(
			expect.arrayContaining(["appMenu", "fileMenu", "editMenu", "windowMenu"]),
		);
		expect(macViewSubmenu()).toContainEqual(expect.objectContaining({ role: "forceReload" }));
	});

	it("does not expose application zoom controls", () => {
		expect(macViewSubmenu()).not.toContainEqual(
			expect.objectContaining({ role: expect.stringMatching(/^zoom|resetZoom$/) }),
		);
	});
});
