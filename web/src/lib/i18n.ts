// i18n configuration and utilities
// This file provides infrastructure for future multilingual support

export type Language = 'zh' | 'en';

export interface Translation {
  // Navigation
  nav: {
    title: string;
    tagline: string;
    login: string;
    welcome: string;
    adminPanel: string;
  };
  
  // Hero section
  hero: {
    title: string;
    subtitle: string;
    downloadClient: string;
    learnMore: string;
  };
  
  // Features section
  features: {
    title: string;
    subtitle: string;
    security: {
      title: string;
      description: string;
    };
    speed: {
      title: string;
      description: string;
    };
    global: {
      title: string;
      description: string;
    };
    multiplatform: {
      title: string;
      description: string;
    };
    opensource: {
      title: string;
      description: string;
    };
    support: {
      title: string;
      description: string;
    };
  };
  
  // Download section
  download: {
    title: string;
    subtitle: string;
    platforms: {
      ios: string;
      android: string;
      windows: string;
      macos: string;
    };
    downloadButton: string;
  };

  // Router products section
  routers: {
    title: string;
    subtitle: string;
    presaleTag: string;
    comingSoon: string;
    learnMore: string;
    benefits: {
      title: string;
      subtitle: string;
      items: {
        easySetup: {
          title: string;
          description: string;
        };
        familyFriendly: {
          title: string;
          description: string;
        };
        alwaysOn: {
          title: string;
          description: string;
        };
        multiDevice: {
          title: string;
          description: string;
        };
        techSupport: {
          title: string;
          description: string;
        };
        costEffective: {
          title: string;
          description: string;
        };
      };
    };
  };
  
  // Open source section
  opensource: {
    title: string;
    subtitle: string;
    server: {
      title: string;
      description: string;
    };
    protocol: {
      title: string;
      description: string;
    };
    viewCode: string;
  };
  
  // Footer
  footer: {
    product: {
      title: string;
      clientDownload: string;
      nodeStatus: string;
    };
    developer: {
      title: string;
      apiDocs: string;
      sourceCode: string;
      techBlog: string;
    };
    support: {
      title: string;
      userGuide: string;
      faq: string;
      contact: string;
    };
    copyright: string;
  };
  
  // Login page
  login: {
    title: string;
    tagline: string;
    email: string;
    emailPlaceholder: string;
    verificationCode: string;
    codePlaceholder: string;
    sendCode: string;
    sendingCode: string;
    loginButton: string;
    loggingIn: string;
    changeEmail: string;
    codeSuccess: string;
    loginSuccess: string;
    enterEmail: string;
  };
}

// Default language (Chinese)
export const zhTranslations: Translation = {
  nav: {
    title: '开途 Kaitu',
    tagline: '安全便捷的网络代理解决方案',
    login: '登录',
    welcome: '欢迎',
    adminPanel: '管理后台',
  },
  hero: {
    title: '安全便捷的 网络代理 解决方案',
    subtitle: '开途（Kaitu）提供专业级的网络代理服务，支持多平台客户端，采用先进的加密技术确保您的网络连接安全、快速、稳定。',
    downloadClient: '下载客户端',
    learnMore: '了解更多',
  },
  features: {
    title: '为什么选择开途？',
    subtitle: '我们专注于为用户提供最优质的网络代理体验',
    security: {
      title: '安全加密',
      description: '采用军用级加密算法，保护您的隐私和数据安全，让您放心访问任何网站。',
    },
    speed: {
      title: '高速稳定',
      description: '全球多节点部署，智能路由选择，确保您获得最佳的网络连接速度。',
    },
    global: {
      title: '全球节点',
      description: '覆盖全球主要国家和地区，随时随地享受自由的网络体验。',
    },
    multiplatform: {
      title: '多平台支持',
      description: '支持 Windows、macOS、iOS、Android 等主流平台，一账号多设备使用。',
    },
    opensource: {
      title: '开源透明',
      description: '服务器端和协议完全开源，代码透明可审计，值得信赖的技术方案。',
    },
    support: {
      title: '专业服务',
      description: '7x24小时技术支持，专业团队为您解决任何使用问题。',
    },
  },
  download: {
    title: '下载客户端',
    subtitle: '选择适合您设备的客户端，开始安全的网络之旅',
    platforms: {
      ios: 'iPhone/iPad',
      android: '安卓手机',
      windows: 'PC 桌面版',
      macos: 'Mac 桌面版',
    },
    downloadButton: '下载',
  },
  routers: {
    title: '智能路由器产品',
    subtitle: '专为家庭和小型办公环境设计的智能路由器解决方案',
    presaleTag: '预售中',
    comingSoon: '即将上线',
    learnMore: '了解详情',
    benefits: {
      title: '为什么选择开途路由器？',
      subtitle: '让网络使用变得更简单、更安全、更智能',
      items: {
        easySetup: {
          title: '即插即用',
          description: '无需复杂配置，插上电源即可使用，老人小孩都能轻松操作',
        },
        familyFriendly: {
          title: '全家共享',
          description: '多设备同时连接，全家人都能享受稳定快速的网络连接',
        },
        alwaysOn: {
          title: '24小时在线',
          description: '路由器持续工作，无需每次开关机，网络服务随时可用',
        },
        multiDevice: {
          title: '多设备支持',
          description: '支持手机、电脑、平板、智能家居等多种设备同时连接',
        },
        techSupport: {
          title: '技术支持',
          description: '专业技术团队提供远程支持，解决您的任何使用问题',
        },
        costEffective: {
          title: '性价比高',
          description: '一次购买，长期使用，比手机客户端更经济实惠',
        },
      },
    },
  },
  opensource: {
    title: '开源项目',
    subtitle: '开途致力于开源透明，所有核心代码均可在 GitHub 上查看和审计',
    server: {
      title: '服务器端',
      description: '完整的服务器端实现，包括用户管理、节点管理、支付集成等功能',
    },
    protocol: {
      title: '通信协议',
      description: '自研的高效安全通信协议，保证数据传输的安全性和稳定性',
    },
    viewCode: '查看代码',
  },
  footer: {
    product: {
      title: '产品',
      clientDownload: '客户端下载',
      nodeStatus: '节点状态',
    },
    developer: {
      title: '开发者',
      apiDocs: 'API 文档',
      sourceCode: '开源代码',
      techBlog: '技术博客',
    },
    support: {
      title: '支持',
      userGuide: '使用指南',
      faq: '常见问题',
      contact: '联系我们',
    },
    copyright: '© 2024 开途 Kaitu. 保留所有权利.',
  },
  login: {
    title: '开途 Kaitu',
    tagline: '安全便捷的网络代理解决方案',
    email: '邮箱地址',
    emailPlaceholder: 'user@example.com',
    verificationCode: '验证码',
    codePlaceholder: '请输入6位验证码',
    sendCode: '发送验证码',
    sendingCode: '发送中...',
    loginButton: '登录',
    loggingIn: '登录中...',
    changeEmail: '返回更改邮箱',
    codeSuccess: '验证码已发送，请检查您的邮箱。',
    loginSuccess: '登录成功！',
    enterEmail: '请输入邮箱地址。',
  },
};

// English translations (prepared for future use)
export const enTranslations: Translation = {
  nav: {
    title: 'Kaitu',
    tagline: 'Secure and convenient network proxy solution',
    login: 'Login',
    welcome: 'Welcome',
    adminPanel: 'Admin Panel',
  },
  hero: {
    title: 'Secure and Convenient Network Proxy Solution',
    subtitle: 'Kaitu provides professional network proxy services with multi-platform client support, using advanced encryption technology to ensure your network connections are secure, fast, and stable.',
    downloadClient: 'Download Client',
    learnMore: 'Learn More',
  },
  features: {
    title: 'Why Choose Kaitu?',
    subtitle: 'We focus on providing users with the best network proxy experience',
    security: {
      title: 'Secure Encryption',
      description: 'Military-grade encryption algorithms protect your privacy and data security, allowing you to access any website with confidence.',
    },
    speed: {
      title: 'High Speed & Stability',
      description: 'Global multi-node deployment with intelligent routing selection ensures optimal network connection speeds.',
    },
    global: {
      title: 'Global Nodes',
      description: 'Coverage across major countries and regions worldwide, enjoy free network experience anytime, anywhere.',
    },
    multiplatform: {
      title: 'Multi-platform Support',
      description: 'Support for Windows, macOS, iOS, Android, and other mainstream platforms - one account, multiple devices.',
    },
    opensource: {
      title: 'Open Source & Transparent',
      description: 'Server-side and protocols are completely open source, with transparent and auditable code - a trustworthy technical solution.',
    },
    support: {
      title: 'Professional Service',
      description: '7x24 hour technical support with professional team to solve any usage issues.',
    },
  },
  download: {
    title: 'Download Client',
    subtitle: 'Choose the client that suits your device and start your secure network journey',
    platforms: {
      ios: 'iPhone/iPad',
      android: 'Android Phone',
      windows: 'PC Desktop',
      macos: 'Mac Desktop',
    },
    downloadButton: 'Download',
  },
  routers: {
    title: 'Smart Router Products',
    subtitle: 'Smart router solutions designed for home and small office environments',
    presaleTag: 'Pre-sale',
    comingSoon: 'Coming Soon',
    learnMore: 'Learn More',
    benefits: {
      title: 'Why Choose Kaitu Router?',
      subtitle: 'Making network usage simpler, safer, and smarter',
      items: {
        easySetup: {
          title: 'Plug & Play',
          description: 'No complex configuration required, just plug in and use - easy for everyone',
        },
        familyFriendly: {
          title: 'Family Sharing',
          description: 'Multiple devices can connect simultaneously, stable network for the whole family',
        },
        alwaysOn: {
          title: '24/7 Online',
          description: 'Router works continuously, network service always available without rebooting',
        },
        multiDevice: {
          title: 'Multi-device Support',
          description: 'Supports phones, computers, tablets, smart home devices simultaneously',
        },
        techSupport: {
          title: 'Technical Support',
          description: 'Professional team provides remote support to solve any usage issues',
        },
        costEffective: {
          title: 'Cost Effective',
          description: 'One-time purchase, long-term use, more economical than mobile clients',
        },
      },
    },
  },
  opensource: {
    title: 'Open Source Project',
    subtitle: 'Kaitu is committed to open source transparency, all core code can be viewed and audited on GitHub',
    server: {
      title: 'Server Side',
      description: 'Complete server-side implementation including user management, node management, payment integration and other functions',
    },
    protocol: {
      title: 'Communication Protocol',
      description: 'Self-developed efficient and secure communication protocol ensuring data transmission security and stability',
    },
    viewCode: 'View Code',
  },
  footer: {
    product: {
      title: 'Product',
      clientDownload: 'Client Download',
      nodeStatus: 'Node Status',
    },
    developer: {
      title: 'Developer',
      apiDocs: 'API Documentation',
      sourceCode: 'Source Code',
      techBlog: 'Tech Blog',
    },
    support: {
      title: 'Support',
      userGuide: 'User Guide',
      faq: 'FAQ',
      contact: 'Contact Us',
    },
    copyright: '© 2024 Kaitu. All rights reserved.',
  },
  login: {
    title: 'Kaitu',
    tagline: 'Secure and convenient network proxy solution',
    email: 'Email Address',
    emailPlaceholder: 'user@example.com',
    verificationCode: 'Verification Code',
    codePlaceholder: 'Enter 6-digit verification code',
    sendCode: 'Send Code',
    sendingCode: 'Sending...',
    loginButton: 'Login',
    loggingIn: 'Logging in...',
    changeEmail: 'Change Email',
    codeSuccess: 'Verification code sent, please check your email.',
    loginSuccess: 'Login successful!',
    enterEmail: 'Please enter email address.',
  },
};

// Translation context
export const translations = {
  zh: zhTranslations,
  en: enTranslations,
};

// Current language - can be changed via state management later
export const getCurrentLanguage = (): Language => 'zh'; // Default to Chinese

// Get current translations
export const getTranslations = (lang?: Language): Translation => {
  return translations[lang || getCurrentLanguage()];
};

// Hook for translations (can be enhanced with React Context later)
export const useTranslations = () => {
  const currentLang = getCurrentLanguage();
  const t = translations[currentLang];
  
  return {
    t,
    currentLang,
    // Future: switchLanguage function can be added here
  };
};