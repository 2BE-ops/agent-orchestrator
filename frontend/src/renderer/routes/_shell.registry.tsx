import { createFileRoute } from "@tanstack/react-router";
import { AgentRegistry } from "../components/AgentRegistry";

export const Route = createFileRoute("/_shell/registry")({ component: AgentRegistry });
