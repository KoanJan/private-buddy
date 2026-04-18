import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import zh from './locales/zh.json';
import en from './locales/en.json';

const LANGUAGE_KEY = 'private-buddy-language';

const getSavedLanguage = (): string => {
  const saved = localStorage.getItem(LANGUAGE_KEY);
  if (saved && (saved === 'zh' || saved === 'en')) {
    return saved;
  }
  const browserLang = navigator.language.toLowerCase();
  return browserLang.startsWith('zh') ? 'zh' : 'en';
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
  localStorage.setItem(LANGUAGE_KEY, lang);
};

export const getCurrentLanguage = (): string => {
  return i18n.language;
};

export default i18n;
