import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import type { RegistryDefinition } from "../lib/registry-api";
import { NativeProviderBindings } from "./NativeProviderBindings";
import { RegistryConfigurationCheck } from "./RegistryConfigurationCheck";

const api = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn(), check: vi.fn(), models: vi.fn() }));
const binding = { id: "binding", name: "Native review", harness: "codex", provider: "configured", projectId: "project", enabled: true, revision: 4 };
vi.mock("../lib/registry-api", () => ({ registryQueryRoot: ["registry"], listProviderBindings: async () => ({items:[binding]}),
	createProviderBinding: api.create, updateProviderBinding: api.update, checkRegistryConfiguration: api.check,
	registryProjectsQuery: {queryKey:["projects"],queryFn:async()=>[{id:"project",name:"Example"}]} }));
vi.mock("../hooks/useAgentModelsQuery", () => ({ agentModelsQueryOptions: (harness: string, project: string) => ({queryKey:["models",harness,project],queryFn:()=>api.models(harness,project)}) }));

function Editor() {
	const [definition, setDefinition] = useState<NonNullable<RegistryDefinition["agentType"]>>({harness:"codex",config:{model:"old-model",effort:"high"},instructions:"",skills:[],capabilities:[],maxParallelWorkers:1});
	return <><NativeProviderBindings definition={definition} onChange={(patch)=>setDefinition((old)=>({...old,...patch}))}/><output aria-label="Selected model">{definition.config.model || "Native default"}</output></>;
}
function mount(content: React.ReactNode) {
	return render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})}>{content}</QueryClientProvider>);
}
beforeEach(()=>{
	vi.clearAllMocks();
	api.models.mockResolvedValue({models:[{id:"configured/model",provider:"configured"}]});
	api.create.mockResolvedValue(binding);
});

it("creates a scoped native reference and clears model choices from the previous binding", async()=>{
	const user=userEvent.setup();
	mount(<Editor/>);
	await user.click(screen.getByRole("button",{name:"Create provider binding"}));
	await user.type(screen.getByLabelText("Binding name"),"Native review");
	await screen.findByRole("option",{name:"Example"});
	await user.selectOptions(screen.getByLabelText("Native configuration scope",{exact:true}),"project");
	await waitFor(()=>expect(api.models).toHaveBeenCalledWith("codex","project"));
	await user.selectOptions(screen.getByLabelText("Configured native provider",{exact:true}),"configured");
	await user.type(screen.getByLabelText("Reason for this binding"),"Use configured native provider");
	await user.click(screen.getByRole("button",{name:"Save binding"}));
	await waitFor(()=>expect(api.create).toHaveBeenCalledWith({name:"Native review",harness:"codex",provider:"configured",projectId:"project",reason:"Use configured native provider"}));
	expect(screen.getByLabelText("Local provider reference",{exact:true})).toHaveValue("binding");
	expect(screen.getByLabelText("Selected model")).toHaveTextContent("Native default");
});

it("uses the current revision to disable a shared reference and reports conflicts",async()=>{
	api.update.mockRejectedValue(new Error("Binding changed; reload"));
	const user=userEvent.setup();
	mount(<Editor/>);
	await screen.findByRole("option",{name:/Native review/});
	await user.selectOptions(screen.getByLabelText("Local provider reference",{exact:true}),"binding");
	await user.click(screen.getByRole("button",{name:"Disable binding"}));
	expect(await screen.findByRole("alert")).toHaveTextContent("Binding changed; reload");
	expect(api.update).toHaveBeenCalledWith("binding",expect.objectContaining({enabled:false,expectedRevision:4}));
});

it("reports readiness for an exact version and clears stale results when the project changes",async()=>{
	api.check.mockResolvedValue({ready:false,issues:[{code:"NATIVE_READINESS_UNAVAILABLE",state:"unavailable",message:"Native authentication unavailable"}]});
	const user=userEvent.setup();
	mount(<RegistryConfigurationCheck id="reviewer" version={3}/>);
	await user.click(screen.getByRole("button",{name:"Check native readiness"}));
	expect(await screen.findByRole("status")).toHaveTextContent("Native authentication unavailable");
	expect(api.check).toHaveBeenCalledWith("reviewer",3,"");
	await user.selectOptions(screen.getByLabelText("Validate for project",{exact:true}),"project");
	expect(screen.queryByRole("status")).not.toBeInTheDocument();
	api.check.mockResolvedValue({ready:true,issues:[]});
	await user.click(screen.getByRole("button",{name:"Check native readiness"}));
	expect(await screen.findByRole("status")).toHaveTextContent("checked again when launched");
	expect(api.check).toHaveBeenLastCalledWith("reviewer",3,"project");
});
