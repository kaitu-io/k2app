// ADB guide data — brand-specific steps with screenshot images.
// Images in /public/images/adb-guide/{brand}/ (PNG converted to WebP)

export interface GuideStep {
  image: string;
  titleKey: string; // i18n key within the brand's steps
}

export interface BrandGuide {
  id: string;
  nameKey: string; // i18n key for brand name
  steps: GuideStep[];
}

const BASE = '/images/adb-guide';

export const brandGuides: BrandGuide[] = [
  {
    id: 'huawei',
    nameKey: 'brandHuawei',
    steps: [
      { image: `${BASE}/huawei/01-settings.webp`, titleKey: 'huawei_step1' },
      { image: `${BASE}/huawei/02-about-phone.webp`, titleKey: 'huawei_step2' },
      { image: `${BASE}/huawei/03-system-update.webp`, titleKey: 'huawei_step3' },
      { image: `${BASE}/huawei/04-developer-options-entry.webp`, titleKey: 'huawei_step4' },
      { image: `${BASE}/huawei/05-developer-options.webp`, titleKey: 'huawei_step5' },
      { image: `${BASE}/huawei/06-usb-debugging.webp`, titleKey: 'huawei_step6' },
    ],
  },
  {
    id: 'xiaomi',
    nameKey: 'brandXiaomi',
    steps: [
      { image: `${BASE}/xiaomi/01-desktop-settings.webp`, titleKey: 'xiaomi_step1' },
      { image: `${BASE}/xiaomi/02-my-device.webp`, titleKey: 'xiaomi_step2' },
      { image: `${BASE}/xiaomi/03-all-specs.webp`, titleKey: 'xiaomi_step3' },
      { image: `${BASE}/xiaomi/04-tap-repeatedly.webp`, titleKey: 'xiaomi_step4' },
      { image: `${BASE}/xiaomi/05-more-settings.webp`, titleKey: 'xiaomi_step5' },
      { image: `${BASE}/xiaomi/06-developer-options.webp`, titleKey: 'xiaomi_step6' },
      { image: `${BASE}/xiaomi/07-usb-debugging.webp`, titleKey: 'xiaomi_step7' },
    ],
  },
  {
    id: 'vivo',
    nameKey: 'brandVivo',
    steps: [
      { image: `${BASE}/vivo/01-desktop.webp`, titleKey: 'vivo_step1' },
      { image: `${BASE}/vivo/02-more-settings.webp`, titleKey: 'vivo_step2' },
      { image: `${BASE}/vivo/03-about-phone.webp`, titleKey: 'vivo_step3' },
      { image: `${BASE}/vivo/04-build-number.webp`, titleKey: 'vivo_step4' },
      { image: `${BASE}/vivo/05-developer-options.webp`, titleKey: 'vivo_step5' },
      { image: `${BASE}/vivo/06-enable-developer-options.webp`, titleKey: 'vivo_step6' },
      { image: `${BASE}/vivo/07-usb-debugging.webp`, titleKey: 'vivo_step7' },
    ],
  },
  {
    id: 'oppo',
    nameKey: 'brandOppo',
    steps: [
      { image: `${BASE}/oppo/01-developer-mode.webp`, titleKey: 'oppo_step1' },
      { image: `${BASE}/oppo/02-usb-debugging.webp`, titleKey: 'oppo_step2' },
    ],
  },
  {
    id: 'samsung',
    nameKey: 'brandSamsung',
    steps: [
      { image: `${BASE}/samsung/01-developer-mode.webp`, titleKey: 'samsung_step1' },
      { image: `${BASE}/samsung/02-usb-debugging.webp`, titleKey: 'samsung_step2' },
    ],
  },
];
