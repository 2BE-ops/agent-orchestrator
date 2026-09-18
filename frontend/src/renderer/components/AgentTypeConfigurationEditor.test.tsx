import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { useState } from "react";
import { AgentTypeConfigurationEditor } from "./AgentTypeConfigurationEditor";
import type { RegistryDefinition } from "../lib/registry-api";

const api = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("../lib/api-client", () => ({ apiClient: { GET: api.get }, apiErrorMessage: (error: unknown) => error instanceof Error ? error.message : String(error) }));

function Editor() {
	const [definition, setDefinition] = useState<NonNullable<RegistryDefinition["agentType"]>>({ harness: "claude-code", sessionMode: "tui", config: {}, maxParallelWorkers: 1, instructions: "", capabilities: [], skills: [] });
	return <AgentTypeConfigurationEditor definition={definition} onChange={(patch) => setDefinition((old) => ({ ...old, ...patch }))} />;
}
function mount() {
	return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Editor /></QueryClientProvider>);
}
beforeEach(() => {
	vi.clearAllMocks();
	api.get.mockImplementation(async (path: string) => {
		if (path.endsWith("/provider-bindings")) return {data:{items:[]}};
		if (path.endsWith("/projects")) return {data:{projects:[]}};
		if (path.endsWith("/configuration")) return {data:{harness:"claude-code",fields:[{key:"model",type:"string",options:[]},{key:"permissions",type:"enum",options:["default","auto"]}],sessionModes:["tui","chat"],chatCapabilities:[],capabilityState:"supported"}};
		if (path.endsWith("/models")) return {data:{models:[{id:"configured/review",label:"Configured reviewer",provider:"Native configured"}],customModelEntry:"configured",selectionMode:"catalog",stale:false}};
		return {data:{agents:[{id:"claude-code",effectiveReadiness:"unknown",authentication:{reason:"Authentication has not been checked"}}]}};
	});
});

it("uses native catalog choices and declared permission options", async () => {
	const user = userEvent.setup();
	mount();
	await user.click(await screen.findByRole("button",{name:"Model"}));
	await user.click(await screen.findByRole("menuitem",{name:/Configured reviewer/}));
	expect(screen.getByRole("button",{name:"Model"})).toHaveTextContent("Configured reviewer");
	expect(screen.queryByRole("option",{name:"bypass-permissions"})).not.toBeInTheDocument();
	await user.selectOptions(screen.getByLabelText("Permissions",{exact:true}),"auto");
	expect(screen.getByLabelText("Permissions",{exact:true})).toHaveValue("auto");
	expect(screen.getByText("Authentication has not been checked")).toBeVisible();
});

it("queries capabilities for the selected mode and preserves unknown availability", async () => {
	const user = userEvent.setup();
	mount();
	await screen.findByRole("option",{name:"Chat"});
	api.get.mockResolvedValue({data:{fields:[],sessionModes:["tui","chat"],chatCapabilities:[],capabilityState:"unknown",warning:"Native capabilities unavailable"}});
	await user.selectOptions(screen.getByLabelText("Session interface",{exact:true}),"chat");
	await waitFor(()=>expect(api.get).toHaveBeenCalledWith("/api/v1/agents/{agent}/configuration",expect.objectContaining({params:{path:{agent:"claude-code"},query:{mode:"chat"}}})));
	expect(await screen.findByText("Native capabilities unavailable")).toBeVisible();
	expect(screen.queryByLabelText("Permissions",{exact:true})).not.toBeInTheDocument();
});
