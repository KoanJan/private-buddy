import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import { storage } from './services/storage';
import zh from './locales/zh.json';
import en from './locales/en.json';

const LANGUAGE_KEY = 'private-buddy-language';

const getSavedLanguage = (): string => {
  const saved = storage.getRaw(LANGUAGE_KEY);
  if (saved && (saved === 'zh' || saved === 'en')) {
    return saved;
  }
  return 'en';
};

i18n
  .use(initReactI18next)
  .init({
    resources: {
      zh: { translation: zh },
      en: { translation: en }
    },
    lng: getSavedLanguage(),
    fallbackLng: 'en',
    interpolation: {
      escapeValue: false
    }
  });

export const changeLanguage = (lang: string) => {
  i18n.changeLanguage(lang);
  storage.setRaw(LANGUAGE_KEY, lang);
};

export const getCurrentLanguage = (): string => {
  return i18n.language;
};

export default i18n;
