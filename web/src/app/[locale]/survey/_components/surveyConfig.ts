export type QuestionType = "single" | "text";

export interface ChoiceOption {
  value: string;
  labelKey: string;
  hasOther?: boolean;
}

export interface Question {
  id: string;
  type: QuestionType;
  labelKey: string;
  required: boolean;
  options?: ChoiceOption[];
  placeholderKey?: string;
}

export interface SurveyConfig {
  surveyKey: string;
  subtitleKey: string;
  questions: Question[];
}

export const surveys: Record<string, SurveyConfig> = {
  active_2026q1: {
    surveyKey: "active_2026q1",
    subtitleKey: "survey.subtitle_active",
    questions: [
      {
        id: "q1",
        type: "single",
        labelKey: "survey.active_q1",
        required: true,
        options: [
          { value: "ai_tools", labelKey: "survey.active_q1_a1" },
          { value: "work", labelKey: "survey.active_q1_a2" },
          { value: "streaming", labelKey: "survey.active_q1_a3" },
          { value: "learning", labelKey: "survey.active_q1_a4" },
          { value: "other", labelKey: "survey.other", hasOther: true },
        ],
      },
      {
        id: "q2",
        type: "single",
        labelKey: "survey.active_q2",
        required: true,
        options: [
          { value: "solo", labelKey: "survey.active_q2_a1" },
          { value: "family", labelKey: "survey.active_q2_a2" },
          { value: "friends", labelKey: "survey.active_q2_a3" },
        ],
      },
      {
        id: "q3",
        type: "text",
        labelKey: "survey.active_q3",
        required: false,
        placeholderKey: "survey.active_q3_placeholder",
      },
    ],
  },
  expired_2026q1: {
    surveyKey: "expired_2026q1",
    subtitleKey: "survey.subtitle_expired",
    questions: [
      {
        id: "q1",
        type: "single",
        labelKey: "survey.expired_q1",
        required: true,
        options: [
          { value: "expensive", labelKey: "survey.expired_q1_a1" },
          { value: "unstable", labelKey: "survey.expired_q1_a2" },
          { value: "alternative", labelKey: "survey.expired_q1_a3" },
          { value: "no_need", labelKey: "survey.expired_q1_a4" },
          { value: "forgot", labelKey: "survey.expired_q1_a5" },
        ],
      },
      {
        id: "q2",
        type: "single",
        labelKey: "survey.expired_q2",
        required: true,
        options: [
          { value: "cheaper", labelKey: "survey.expired_q2_a1" },
          { value: "stable", labelKey: "survey.expired_q2_a2" },
          { value: "more_devices", labelKey: "survey.expired_q2_a3" },
          { value: "support", labelKey: "survey.expired_q2_a4" },
          { value: "other", labelKey: "survey.other", hasOther: true },
        ],
      },
      {
        id: "q3",
        type: "single",
        labelKey: "survey.expired_q3",
        required: true,
        options: [
          { value: "more", labelKey: "survey.expired_q3_a1" },
          { value: "same", labelKey: "survey.expired_q3_a2" },
          { value: "maybe_later", labelKey: "survey.expired_q3_a3" },
          { value: "no", labelKey: "survey.expired_q3_a4" },
        ],
      },
    ],
  },
};
