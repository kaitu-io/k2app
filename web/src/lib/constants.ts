// Application constants and configuration
import { siteBrand, type Brand } from './brands';

export function getDownloadLinks(version: string, brand: Brand = siteBrand()) {
  const [primary, backup = primary] = brand.cdn.desktopBases;
  const p = brand.cdn.artifactPrefix;
  return {
    windows: {
      primary: `${primary}/${version}/${p}_${version}_x64.exe`,
      backup: `${backup}/${version}/${p}_${version}_x64.exe`,
    },
    macos: {
      primary: `${primary}/${version}/${p}_${version}_universal.pkg`,
      backup: `${backup}/${version}/${p}_${version}_universal.pkg`,
    },
    linux: {
      primary: `${primary}/${version}/${p}_${version}_linux_amd64.tar.gz`,
      backup: `${backup}/${version}/${p}_${version}_linux_amd64.tar.gz`,
    },
  };
}

export function getAndroidDownloadLinks(version: string, brand: Brand = siteBrand()) {
  const [primary, backup = primary] = brand.cdn.mobileBases;
  const p = brand.cdn.artifactPrefix;
  return {
    primary: `${primary}/android/${version}/${p}-${version}.apk`,
    backup: `${backup}/android/${version}/${p}-${version}.apk`,
  };
}
