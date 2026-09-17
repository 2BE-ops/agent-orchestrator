import { Check, Cloud, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useCloudGate } from "../hooks/useCloudGate";
import { useUpdateCloudOffering } from "../hooks/useSettings";
import { SetupActionButton } from "./SetupActionButton";

/** Step: cloud. Optional and off by default. Enabling it here is what makes the
 *  cloud option available in the project step that follows. */
export function OnboardingCloudStep({ cloudEnabled }: { cloudEnabled: boolean }) {
	const { t } = useTranslation();
	const gate = useCloudGate();
	const offering = useUpdateCloudOffering();
	const enabled = gate.cloudEnabled || cloudEnabled;

	return (
		<div className="flex w-full max-w-[420px] flex-col gap-2 text-left">
			<ul className="flex flex-col gap-1.5 px-1">
				{(["sandboxes", "mobile", "preview"] as const).map((feature) => (
					<li key={feature} className="flex items-start gap-2 text-caption leading-snug text-muted-foreground">
						<Check aria-hidden="true" className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
						{t(`onboarding.cloudFeature.${feature}` as const)}
					</li>
				))}
			</ul>
			{enabled ? (
				<SetupActionButton
					icon={<Check className="text-status-ready" aria-hidden="true" />}
					label={t("onboarding.cloudEnabled")}
					disabled
				/>
			) : (
				<SetupActionButton
					icon={<Cloud aria-hidden="true" />}
					label={offering.saving ? t("onboarding.cloudEnabling") : t("onboarding.cloudEnable")}
					description={t("onboarding.cloudEnableDetail")}
					disabled={offering.saving}
					onClick={() => offering.update(true)}
					trailing={offering.saving ? <Loader2 aria-hidden="true" className="size-3.5 animate-spin motion-reduce:animate-none" /> : undefined}
				/>
			)}
			{offering.error ? (
				<p className="px-4 text-caption leading-snug text-destructive" role="alert">
					{offering.error}
				</p>
			) : null}
		</div>
	);
}
