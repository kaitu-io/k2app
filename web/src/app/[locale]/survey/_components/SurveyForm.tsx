"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Input } from "@/components/ui/input";
import type { SurveyConfig } from "./surveyConfig";

interface SurveyFormProps {
  config: SurveyConfig;
  onSubmit: (answers: Record<string, string>) => void;
  isSubmitting: boolean;
  isAuthenticated: boolean;
}

export default function SurveyForm({ config, onSubmit, isSubmitting, isAuthenticated }: SurveyFormProps) {
  const t = useTranslations();
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [otherTexts, setOtherTexts] = useState<Record<string, string>>({});

  const totalQuestions = config.questions.length;

  const answeredCount = config.questions.filter((q) => {
    const val = answers[q.id];
    if (!val) return false;
    // If "other" is selected, require the text input to be filled
    const option = q.options?.find((o) => o.value === val);
    if (option?.hasOther && !otherTexts[q.id]?.trim()) return false;
    return true;
  }).length;

  const allRequiredAnswered = config.questions
    .filter((q) => q.required)
    .every((q) => {
      const val = answers[q.id];
      if (!val) return false;
      const option = q.options?.find((o) => o.value === val);
      if (option?.hasOther && !otherTexts[q.id]?.trim()) return false;
      return true;
    });

  const handleSubmit = () => {
    const finalAnswers: Record<string, string> = {};
    for (const q of config.questions) {
      const val = answers[q.id];
      if (!val) continue;
      const option = q.options?.find((o) => o.value === val);
      if (option?.hasOther && otherTexts[q.id]?.trim()) {
        finalAnswers[q.id] = `other: ${otherTexts[q.id].trim()}`;
      } else {
        finalAnswers[q.id] = val;
      }
    }
    onSubmit(finalAnswers);
  };

  return (
    <div className="space-y-8">
      {/* Progress */}
      <div className="text-sm text-muted-foreground">
        {t("survey.progress", { current: answeredCount, total: totalQuestions })}
      </div>
      <div className="h-2 rounded-full bg-muted overflow-hidden">
        <div
          className="h-full rounded-full bg-primary transition-all duration-300"
          style={{ width: `${(answeredCount / totalQuestions) * 100}%` }}
        />
      </div>

      {/* Questions */}
      {config.questions.map((question, index) => (
        <div key={question.id} className="space-y-3">
          <Label className="text-base font-medium">
            {index + 1}. {t(question.labelKey)}
            {!question.required && (
              <span className="ml-2 text-sm text-muted-foreground font-normal">
                ({t("survey.optional")})
              </span>
            )}
          </Label>

          {question.type === "single" && question.options && (
            <RadioGroup
              className="space-y-1"
              value={answers[question.id] || ""}
              onValueChange={(value) =>
                setAnswers((prev) => ({ ...prev, [question.id]: value }))
              }
            >
              {question.options.map((option) => (
                <div key={option.value} className="space-y-2">
                  <div className="flex items-center space-x-3 py-2">
                    <RadioGroupItem
                      value={option.value}
                      id={`${question.id}-${option.value}`}
                    />
                    <Label
                      htmlFor={`${question.id}-${option.value}`}
                      className="font-normal cursor-pointer"
                    >
                      {t(option.labelKey)}
                    </Label>
                  </div>
                  {option.hasOther && answers[question.id] === option.value && (
                    <Input
                      className="ml-7"
                      placeholder={t("survey.otherPlaceholder")}
                      value={otherTexts[question.id] || ""}
                      onChange={(e) =>
                        setOtherTexts((prev) => ({
                          ...prev,
                          [question.id]: e.target.value,
                        }))
                      }
                    />
                  )}
                </div>
              ))}
            </RadioGroup>
          )}

          {question.type === "text" && (
            <Textarea
              placeholder={question.placeholderKey ? t(question.placeholderKey) : ""}
              value={answers[question.id] || ""}
              onChange={(e) =>
                setAnswers((prev) => ({ ...prev, [question.id]: e.target.value }))
              }
              rows={3}
            />
          )}
        </div>
      ))}

      {/* Submit */}
      <Button
        className="w-full"
        size="lg"
        onClick={handleSubmit}
        disabled={!allRequiredAnswered || isSubmitting || !isAuthenticated}
      >
        {!isAuthenticated
          ? t("survey.login_to_submit")
          : isSubmitting
            ? t("survey.submitting")
            : t("survey.submit")}
      </Button>
    </div>
  );
}
