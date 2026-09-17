import * as Dialog from "@radix-ui/react-dialog";
import { useTranslation } from "react-i18next";
import { useCloudSession } from "../lib/cloud-session";
import { CloudProjectCard, CloudSignInPanel } from "./CreateProjectFlow";

/** Creating a cloud project from onboarding, presented the way the app presents
 *  Clone Repository: one modal over the flow. The card inside owns the surface
 *  (its own border, shadow, and close button), so this wrapper only positions
 *  and traps focus, matching the clone dialog's geometry. */
export function OnboardingCloudDialog({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
	const { t } = useTranslation();
	const { status, signIn } = useCloudSession();
	const signedIn = status === "authenticated";

	return (
		<Dialog.Root open onOpenChange={(next) => { if (!next) onClose(); }}>
			<Dialog.Portal>
				<Dialog.Overlay className="dialog-overlay z-[calc(var(--z-overlay)-1)] data-[state=open]:animate-overlay-in data-[state=closed]:animate-overlay-out" />
				<Dialog.Content className="fixed left-1/2 top-1/2 z-overlay flex max-h-[min(640px,calc(100svh-24px))] w-[min(560px,calc(100vw-24px))] -translate-x-1/2 -translate-y-1/2 flex-col overflow-hidden focus:outline-none data-[state=open]:animate-modal-in data-[state=closed]:animate-modal-out motion-reduce:animate-none">
					<Dialog.Title className="sr-only">{t("onboarding.createCloudProject")}</Dialog.Title>
					<Dialog.Description className="sr-only">{t("onboarding.cloudDialogDescription")}</Dialog.Description>
					<div className="min-h-0 overflow-y-auto">
						{signedIn ? (
							<CloudProjectCard dialog onClose={onClose} onCreated={onCreated} />
						) : (
							<CloudSignInPanel dialog disabled={false} onSignIn={signIn} />
						)}
					</div>
				</Dialog.Content>
			</Dialog.Portal>
		</Dialog.Root>
	);
}
