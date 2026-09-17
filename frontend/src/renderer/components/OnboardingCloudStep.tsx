import { Check, Cloud } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useCloudGate } from "../hooks/useCloudGate";
import { useUpdateCloudOffering } from "../hooks/useSettings";
import { SetupList, SetupRow } from "./SetupList";

/** Step: cloud. One decision, so it reads as two options rather than a switch
 *  plus explanatory prose. The choice applies optimistically: the row marks
 *  itself selected and the daemon is told in the background. */
export function OnboardingCloudStep({ cloudEnabled, onChoose }: { cloudEnabled: boolean; onChoose: () => void }) {
	const { t } = useTranslation();
	const gate = useCloudGate();
	const offering = useUpdateCloudOffering();
	const enabled = gate.cloudEnabled || cloudEnabled;
	const [choice, setChoice] = useState<boolean | null>(null);
	const selected = choice ?? enabled;

	const choose = (next: boolean) => {
		setChoice(next);
		offering.update(next);
		onChoose();
	};

	return (
		<div className="flex w-full max-w-[440px] flex-col gap-4 text-left">
			<SetupList>
				<SetupRow
					icon={<Cloud aria-hidden="true" />}
					label={t("onboarding.cloudOptionYesLabel")}
					description={t("onboarding.cloudOptionYesDetail")}
					selected={selected}
					trailing={selected ? <Check aria-hidden="true" className="size-3.5 text-status-ready" /> : undefined}
					onClick={() => choose(true)}
				/>
				<SetupRow
					icon={<Cloud aria-hidden="true" />}
					label={t("onboarding.cloudOptionNoLabel")}
					description={t("onboarding.cloudOptionNoDetail")}
					selected={!selected}
					trailing={!selected ? <Check aria-hidden="true" className="size-3.5 text-status-ready" /> : undefined}
					onClick={() => choose(false)}
				/>
			</SetupList>
			<p className="px-1 text-center text-caption leading-snug text-muted-foreground/80">{t("onboarding.cloudCaveat")}</p>
			{offering.error ? (
				<p className="px-1 text-center text-caption leading-snug text-destructive" role="alert">
					{offering.error}
				</p>
			) : null}
		</div>
	);
}
