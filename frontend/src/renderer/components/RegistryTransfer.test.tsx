import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { RegistryExport, RegistryImport } from "./RegistryTransfer";

const api = vi.hoisted(() => ({ import: vi.fn(), export: vi.fn() }));
vi.mock("../lib/registry-api", () => ({ importRegistry: api.import, exportRegistry: api.export }));
vi.mock("../lib/api-client", () => ({ apiErrorMessage: (error: unknown) => error instanceof Error ? error.message : String(error) }));
const bundle = { schemaVersion: 1, kind: "skill", name: "Review", description: "Check", skill: { instructions: "Review", capabilities: [], requiredTools: ["git"], requiredMcpServers: [], resources: [] } };
function mount(content: React.ReactNode) {
	return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}>{content}</QueryClientProvider>);
}
beforeEach(() => vi.clearAllMocks());

it("previews requirements and imports only on explicit submit, preserving a rejected draft", async () => {
	api.import.mockRejectedValueOnce(new Error("Invalid resource path")).mockResolvedValueOnce({ root: { entry: { id: "new" } }, requirements: ["Review before enabling"] });
	const onImported = vi.fn();
	const user = userEvent.setup();
	mount(<RegistryImport kind="skill" onCancel={vi.fn()} onImported={onImported} />);
	fireEvent.change(screen.getByLabelText("Portable bundle JSON"), { target: { value: JSON.stringify(bundle) } });
	expect(screen.getByRole("region", { name: "Import preview" })).toHaveTextContent("Requires: git");
	expect(api.import).not.toHaveBeenCalled();
	await user.type(screen.getByLabelText("Reason for this change"), "Import reviewed content");
	await user.click(screen.getByRole("button", { name: "Import as disabled" }));
	expect(await screen.findByRole("alert")).toHaveTextContent("Invalid resource path");
	expect(screen.getByLabelText("Portable bundle JSON")).toHaveValue(JSON.stringify(bundle));
	await user.click(screen.getByRole("button", { name: "Import as disabled" }));
	await waitFor(() => expect(onImported).toHaveBeenCalledWith("new", ["Review before enabling"]));
	expect(api.import).toHaveBeenCalledWith("skill", { bundle, reason: "Import reviewed content" });
});

it("rejects malformed preview structures without crashing or mutating", () => {
	mount(<RegistryImport kind="agent_type" onCancel={vi.fn()} onImported={vi.fn()} />);
	for (const value of ["null", "{", JSON.stringify({ ...bundle, kind: "agent_type", agentType: { harness: "codex", skills: "bad" } }), JSON.stringify({ ...bundle, kind: "agent_type", agentType: { harness: "codex", skills: [{name:"bad",definition:{requiredTools:[{}]}}] } })]) {
		fireEvent.change(screen.getByLabelText("Portable bundle JSON"), { target: { value } });
		expect(screen.getByRole("alert")).toBeVisible();
		expect(screen.getByRole("button", { name: "Import as disabled" })).toBeDisabled();
	}
	expect(api.import).not.toHaveBeenCalled();
});

it("exports the exact selected version for inspection and download", async () => {
	api.export.mockResolvedValue(bundle);
	const user = userEvent.setup();
	mount(<RegistryExport kind="skill" id="s1" version={3} />);
	await user.click(screen.getByRole("button", { name: "Export active version" }));
	expect(await screen.findByLabelText("Exported bundle JSON")).toHaveValue(JSON.stringify(bundle, null, 2));
	expect(api.export).toHaveBeenCalledWith("skill", "s1", 3);
	expect(screen.getByRole("button", { name: "Download JSON" })).toBeEnabled();
});
