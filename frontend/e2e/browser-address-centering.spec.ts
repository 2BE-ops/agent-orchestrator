import { expect, test, type Page } from "@playwright/test";

async function expectAddressCentered(page: Page) {
	await expect
		.poll(async () => {
			const inspectorTopbar = await page.locator("#inspector .session-inspector__topbar").boundingBox();
			const address = await page.getByTestId("browser-address-bar").boundingBox();
			if (!inspectorTopbar || !address) return Number.POSITIVE_INFINITY;
			return Math.abs(address.x + address.width / 2 - (inspectorTopbar.x + inspectorTopbar.width / 2));
		})
		.toBeLessThanOrEqual(1);
}

test("@P0 browser address remains centered at normal and enlarged renderer scales", async ({ page }) => {
	await page.goto("/#/projects/ao-demo/sessions/demo-working");
	await page.locator("#inspector").getByRole("tab", { name: "Browser" }).click();
	await expect(page.getByTestId("browser-address-bar")).toBeVisible();

	await expectAddressCentered(page);
	await page.setViewportSize({ width: 960, height: 720 });
	await expectAddressCentered(page);
});
