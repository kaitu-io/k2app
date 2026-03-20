export interface GuideStep {
  image?: string;
  titleKey: string;
  descriptionKey: string;
}

export interface BrandGuide {
  id: string;
  labelKey: string;
  steps: GuideStep[];
}

export const BRAND_GUIDES: BrandGuide[] = [
  {
    id: "xiaomi",
    labelKey: "install.install.androidGuides.xiaomiLabel",
    steps: [
      { image: "/images/install/xiaomi/step1.png", titleKey: "install.install.androidGuides.xiaomiStep1Title", descriptionKey: "install.install.androidGuides.xiaomiStep1Desc" },
      { image: "/images/install/xiaomi/step2.png", titleKey: "install.install.androidGuides.xiaomiStep2Title", descriptionKey: "install.install.androidGuides.xiaomiStep2Desc" },
      { image: "/images/install/xiaomi/step3.png", titleKey: "install.install.androidGuides.xiaomiStep3Title", descriptionKey: "install.install.androidGuides.xiaomiStep3Desc" },
      { image: "/images/install/xiaomi/step4.png", titleKey: "install.install.androidGuides.xiaomiStep4Title", descriptionKey: "install.install.androidGuides.xiaomiStep4Desc" },
    ],
  },
  {
    id: "huawei",
    labelKey: "install.install.androidGuides.huaweiLabel",
    steps: [
      { image: "/images/install/huawei/step1.png", titleKey: "install.install.androidGuides.huaweiStep1Title", descriptionKey: "install.install.androidGuides.huaweiStep1Desc" },
      { image: "/images/install/huawei/step2.png", titleKey: "install.install.androidGuides.huaweiStep2Title", descriptionKey: "install.install.androidGuides.huaweiStep2Desc" },
      { image: "/images/install/huawei/step3.png", titleKey: "install.install.androidGuides.huaweiStep3Title", descriptionKey: "install.install.androidGuides.huaweiStep3Desc" },
      { image: "/images/install/huawei/step4.png", titleKey: "install.install.androidGuides.huaweiStep4Title", descriptionKey: "install.install.androidGuides.huaweiStep4Desc" },
    ],
  },
  {
    id: "oppoVivo",
    labelKey: "install.install.androidGuides.oppoVivoLabel",
    steps: [
      { image: "/images/install/oppo-vivo/step1.png", titleKey: "install.install.androidGuides.oppoVivoStep1Title", descriptionKey: "install.install.androidGuides.oppoVivoStep1Desc" },
      { image: "/images/install/oppo-vivo/step2.png", titleKey: "install.install.androidGuides.oppoVivoStep2Title", descriptionKey: "install.install.androidGuides.oppoVivoStep2Desc" },
      { image: "/images/install/oppo-vivo/step3.png", titleKey: "install.install.androidGuides.oppoVivoStep3Title", descriptionKey: "install.install.androidGuides.oppoVivoStep3Desc" },
      { image: "/images/install/oppo-vivo/step4.png", titleKey: "install.install.androidGuides.oppoVivoStep4Title", descriptionKey: "install.install.androidGuides.oppoVivoStep4Desc" },
    ],
  },
  {
    id: "desktopUsb",
    labelKey: "install.install.androidGuides.desktopUsbLabel",
    steps: [],
  },
  {
    id: "generic",
    labelKey: "install.install.androidGuides.genericLabel",
    steps: [
      { titleKey: "install.install.androidGuides.genericStep1Title", descriptionKey: "install.install.androidGuides.genericStep1Desc" },
      { titleKey: "install.install.androidGuides.genericStep2Title", descriptionKey: "install.install.androidGuides.genericStep2Desc" },
      { titleKey: "install.install.androidGuides.genericStep3Title", descriptionKey: "install.install.androidGuides.genericStep3Desc" },
      { titleKey: "install.install.androidGuides.genericStep4Title", descriptionKey: "install.install.androidGuides.genericStep4Desc" },
      { titleKey: "install.install.androidGuides.genericStep5Title", descriptionKey: "install.install.androidGuides.genericStep5Desc" },
    ],
  },
];

export function detectDefaultTab(ua: string): string {
  const lower = ua.toLowerCase();
  const isAndroid = /android/.test(lower);

  if (!isAndroid) {
    return "desktopUsb";
  }

  if (/xiaomi|redmi|miui|poco/.test(lower)) return "xiaomi";
  if (/huawei|honor|hmscore/.test(lower)) return "huawei";
  if (/oppo|realme|oneplus/.test(lower)) return "oppoVivo";
  if (/vivo/.test(lower)) return "oppoVivo";

  return "generic";
}
