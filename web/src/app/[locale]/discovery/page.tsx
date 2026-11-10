import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { generateMetadata as generateBaseMetadata } from '../metadata';
import { routing } from '@/i18n/routing';
import { Link } from '@/i18n/routing';
import { Card } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import {
  Globe,
  Smartphone,
  Video,
  ExternalLink,
  Users,
  BookOpen,
  ShoppingBag,
  Tv,
  Newspaper
} from 'lucide-react';
import DiscoveryClient from './DiscoveryClient';
import { ROUTER_EDITION_ON_SALE } from '@/lib/router-edition';

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

const DISCOVERY_DATA = {
  // Conservative News Sources
  news: [
    {
      name: "Wall Street Journal",
      url: "https://www.wsj.com",
      icon: "📈",
      description: "美国权威财经报纸，提供高质量的商业和政治新闻报道"
    },
    {
      name: "National Review",
      url: "https://www.nationalreview.com",
      icon: "🗽",
      description: "美国保守主义思想重镇，提供深度政治文化分析"
    },
    {
      name: "The Federalist",
      url: "https://thefederalist.com",
      icon: "📜",
      description: "保守派思想门户网站，以深度分析和评论著称"
    },
    {
      name: "Daily Wire",
      url: "https://www.dailywire.com",
      icon: "⚡",
      description: "快速增长的保守派媒体平台，提供新闻和评论内容"
    },
    {
      name: "Fox News",
      url: "https://www.foxnews.com",
      icon: "🦊",
      description: "美国收视率最高的有线新闻频道"
    },
    {
      name: "Epoch Times",
      url: "https://www.theepochtimes.com",
      icon: "🌐",
      description: "独立新闻媒体，专注中国问题和国际新闻报道"
    },
    {
      name: "Just the News",
      url: "https://justthenews.com",
      icon: "📰",
      description: "专注事实报道的新闻网站，提供原创调查报道"
    },
    {
      name: "New York Post",
      url: "https://nypost.com",
      icon: "🗞️",
      description: "纽约历史悠久的小报，以快速新闻报道著称"
    }
  ],

  // Entertainment Platforms
  entertainment: [
    {
      name: "YouTube",
      url: "https://www.youtube.com",
      icon: "📺",
      description: "全球最大的视频分享平台，拥有丰富的教育和娱乐内容"
    },
    {
      name: "Netflix",
      url: "https://www.netflix.com",
      icon: "🎬",
      description: "全球领先的流媒体娱乐服务，提供原创剧集和电影"
    },
    {
      name: "Spotify",
      url: "https://www.spotify.com",
      icon: "🎧",
      description: "全球最大的音乐流媒体平台，支持播客和有声书"
    },
    {
      name: "Prime Video",
      url: "https://www.primevideo.com",
      icon: "📽️",
      description: "亚马逊旗下流媒体服务，提供独家内容和电影租赁"
    },
    {
      name: "Apple Music",
      url: "https://music.apple.com",
      icon: "🍎",
      description: "苹果音乐服务，高品质音频和独家内容"
    },
    {
      name: "Twitch",
      url: "https://www.twitch.tv",
      icon: "🎮",
      description: "游戏直播平台，实时互动娱乐内容"
    }
  ],

  // Communication Apps
  communication: [
    {
      name: "WhatsApp",
      url: "https://www.whatsapp.com",
      icon: "💬",
      description: "全球最受欢迎的即时通讯应用，支持语音和视频通话"
    },
    {
      name: "Telegram",
      url: "https://telegram.org",
      icon: "✈️",
      description: "注重隐私安全的云端通讯应用，支持大型群组"
    },
    {
      name: "Signal",
      url: "https://signal.org",
      icon: "🔒",
      description: "开源加密通讯应用，被隐私专家广泛推荐"
    },
    {
      name: "Discord",
      url: "https://discord.com",
      icon: "🎮",
      description: "社区聊天平台，支持语音频道和服务器管理"
    },
    {
      name: "Zoom",
      url: "https://zoom.us",
      icon: "📹",
      description: "专业视频会议软件，支持网络研讨会和在线课程"
    },
    {
      name: "Microsoft Teams",
      url: "https://teams.microsoft.com",
      icon: "👥",
      description: "企业级协作平台，集成办公套件和项目管理"
    }
  ],

  // Recommended Applications
  applications: [
    {
      name: "Notion",
      url: "https://www.notion.so",
      icon: "📝",
      description: "一体化工作空间，笔记、项目管理和知识库的完美结合"
    },
    {
      name: "Canva",
      url: "https://www.canva.com",
      icon: "🎨",
      description: "简单易用的在线设计工具，模板丰富适合非专业人士"
    },
    {
      name: "Google Workspace",
      url: "https://workspace.google.com",
      icon: "☁️",
      description: "谷歌办公套件，包含文档、表格、演示文稿和云存储"
    },
    {
      name: "Figma",
      url: "https://www.figma.com",
      icon: "🖼️",
      description: "专业的协作式UI/UX设计工具，支持实时团队协作"
    },
    {
      name: "1Password",
      url: "https://1password.com",
      icon: "🔐",
      description: "安全的密码管理器，保护您的数字身份和敏感信息"
    },
    {
      name: "Grammarly",
      url: "https://www.grammarly.com",
      icon: "✍️",
      description: "AI写作助手，帮助改善英语写作的语法和表达"
    }
  ],

  // Evangelical Christian Resources
  christian: [
    {
      name: "Desiring God",
      url: "https://www.desiringgod.org",
      icon: "🙏",
      description: "约翰·派博创办的福音派事工，传播神在万有中的至高无上"
    },
    {
      name: "Ligonier Ministries",
      url: "https://www.ligonier.org",
      icon: "✝️",
      description: "R.C. Sproul创办的教导事工，专注于神的圣洁和改革宗神学"
    },
    {
      name: "Grace to You",
      url: "https://www.gty.org",
      icon: "📖",
      description: "约翰·麦克阿瑟的教导事工，提供逐节解经和圣经研究"
    },
    {
      name: "Got Questions",
      url: "https://www.gotquestions.org",
      icon: "❓",
      description: "全球最大的圣经问答网站，提供多语言圣经解答"
    },
    {
      name: "The Gospel Coalition",
      url: "https://www.thegospelcoalition.org",
      icon: "🤝",
      description: "福音联盟，汇聚福音派牧师和神学家的协作平台"
    },
    {
      name: "Christianity Today",
      url: "https://www.christianitytoday.com",
      icon: "📰",
      description: "权威基督教新闻杂志，报道全球基督教动态"
    },
    {
      name: "Bible Gateway",
      url: "https://www.biblegateway.com",
      icon: "📱",
      description: "全球最受欢迎的在线圣经平台，支持多种译本"
    }
  ],

  // AI Tools and Platforms
  aiTools: [
    {
      name: "ChatGPT",
      url: "https://chat.openai.com",
      icon: "🤖",
      description: "OpenAI开发的AI对话助手，支持创作、编程和问答"
    },
    {
      name: "Claude",
      url: "https://claude.ai",
      icon: "🧠",
      description: "Anthropic开发的AI助手，专注安全有用的对话"
    },
    {
      name: "GitHub Copilot",
      url: "https://github.com/features/copilot",
      icon: "💻",
      description: "AI编程助手，实时代码建议和自动完成"
    },
    {
      name: "Midjourney",
      url: "https://www.midjourney.com",
      icon: "🎨",
      description: "AI图像生成工具，创造高质量的艺术作品"
    },
    {
      name: "Runway",
      url: "https://runwayml.com",
      icon: "🎬",
      description: "AI视频编辑和生成平台，适合创意工作者"
    },
    {
      name: "Perplexity",
      url: "https://www.perplexity.ai",
      icon: "🔍",
      description: "AI搜索引擎，提供引用来源的准确答案"
    }
  ],

  // Foreign Trade Tools
  tradeTools: [
    {
      name: "Alibaba",
      url: "https://www.alibaba.com",
      icon: "🏭",
      description: "全球最大的B2B贸易平台，连接买家和供应商"
    },
    {
      name: "Global Sources",
      url: "https://www.globalsources.com",
      icon: "🌐",
      description: "专业的B2B采购平台，专注亚洲制造商"
    },
    {
      name: "Made-in-China",
      url: "https://www.made-in-china.com",
      icon: "🇨🇳",
      description: "中国制造商门户网站，提供产品展示和贸易服务"
    },
    {
      name: "TradeKey",
      url: "https://www.tradekey.com",
      icon: "🔑",
      description: "国际贸易门户，连接全球进出口商"
    },
    {
      name: "ExportHub",
      url: "https://www.exporthub.com",
      icon: "📦",
      description: "全球贸易网络，专注出口贸易机会"
    },
    {
      name: "EC21",
      url: "https://www.ec21.com",
      icon: "🏢",
      description: "韩国领先的B2B贸易平台，连接亚洲供应商"
    }
  ],

  // Homeschool Education Resources
  homeschool: [
    {
      name: "Khan Academy",
      url: "https://www.khanacademy.org",
      icon: "🎓",
      description: "免费的世界级教育，涵盖从小学到大学的所有学科"
    },
    {
      name: "Time4Learning",
      url: "https://www.time4learning.com",
      icon: "⏰",
      description: "在线家庭教育课程，适合K-12年级的完整curriculum"
    },
    {
      name: "IXL Learning",
      url: "https://www.ixl.com",
      icon: "📊",
      description: "个性化学习平台，提供数学、语言艺术等学科练习"
    },
    {
      name: "Homeschool.com",
      url: "https://www.homeschool.com",
      icon: "🏠",
      description: "家庭教育资源门户，提供课程、资源和社区支持"
    },
    {
      name: "Easy Peasy All-in-One",
      url: "https://allinonehomeschool.com",
      icon: "📚",
      description: "完全免费的基督教家庭教育课程，从学前到高中"
    },
    {
      name: "Teaching Textbooks",
      url: "https://www.teachingtextbooks.com",
      icon: "🔢",
      description: "互动式数学教程，专为家庭教育设计"
    },
    {
      name: "The Good and the Beautiful",
      url: "https://www.goodandbeautiful.com",
      icon: "🌟",
      description: "高质量、低成本的家庭教育课程和资源"
    }
  ],
};

/**
 * Per-locale site descriptions for the foreign locales, keyed by site URL.
 * zh-CN / zh-TW / zh-HK (and any locale missing here) keep the Simplified
 * `description` in DISCOVERY_DATA verbatim.
 */
type ForeignLocale = 'en-US' | 'ko' | 'ja' | 'ru';

const DESCRIPTIONS_I18N: Record<ForeignLocale, Record<string, string>> = {
  'en-US': {
    "https://www.wsj.com": "Leading US financial newspaper with high-quality business and political reporting",
    "https://www.nationalreview.com": "A flagship of American conservative thought, offering in-depth political and cultural analysis",
    "https://thefederalist.com": "Conservative web magazine known for in-depth analysis and commentary",
    "https://www.dailywire.com": "Fast-growing conservative media platform with news and commentary",
    "https://www.foxnews.com": "The most-watched cable news channel in the US",
    "https://www.theepochtimes.com": "Independent news outlet focused on China and international news",
    "https://justthenews.com": "Fact-focused news site with original investigative reporting",
    "https://nypost.com": "Long-running New York tabloid known for fast news coverage",
    "https://www.youtube.com": "The world's largest video-sharing platform, rich in educational and entertainment content",
    "https://www.netflix.com": "Leading global streaming service with original series and films",
    "https://www.spotify.com": "The world's largest music streaming platform, with podcasts and audiobooks",
    "https://www.primevideo.com": "Amazon's streaming service, with exclusive content and movie rentals",
    "https://music.apple.com": "Apple's music service, with high-quality audio and exclusive content",
    "https://www.twitch.tv": "Live game-streaming platform with real-time interactive entertainment",
    "https://www.whatsapp.com": "The world's most popular messaging app, with voice and video calls",
    "https://telegram.org": "Privacy-focused cloud messaging app that supports large groups",
    "https://signal.org": "Open-source encrypted messaging app widely recommended by privacy experts",
    "https://discord.com": "Community chat platform with voice channels and server management",
    "https://zoom.us": "Professional video conferencing software for webinars and online classes",
    "https://teams.microsoft.com": "Enterprise collaboration platform integrating an office suite and project management",
    "https://www.notion.so": "All-in-one workspace combining notes, project management and a knowledge base",
    "https://www.canva.com": "Easy online design tool with lots of templates, great for non-designers",
    "https://workspace.google.com": "Google's office suite: Docs, Sheets, Slides and cloud storage",
    "https://www.figma.com": "Professional collaborative UI/UX design tool with real-time teamwork",
    "https://1password.com": "Secure password manager that protects your digital identity and sensitive data",
    "https://www.grammarly.com": "AI writing assistant that improves English grammar and phrasing",
    "https://www.desiringgod.org": "Evangelical ministry founded by John Piper, spreading a passion for the supremacy of God in all things",
    "https://www.ligonier.org": "Teaching ministry founded by R.C. Sproul, focused on the holiness of God and Reformed theology",
    "https://www.gty.org": "John MacArthur's teaching ministry, offering verse-by-verse exposition and Bible study",
    "https://www.gotquestions.org": "The world's largest Bible Q&A site, with answers in many languages",
    "https://www.thegospelcoalition.org": "The Gospel Coalition, a collaborative platform of evangelical pastors and theologians",
    "https://www.christianitytoday.com": "Respected Christian news magazine covering Christianity worldwide",
    "https://www.biblegateway.com": "The world's most popular online Bible, with many translations",
    "https://chat.openai.com": "AI chat assistant by OpenAI for writing, coding and Q&A",
    "https://claude.ai": "AI assistant by Anthropic, focused on safe and helpful conversation",
    "https://github.com/features/copilot": "AI coding assistant with real-time code suggestions and autocomplete",
    "https://www.midjourney.com": "AI image generator for high-quality artwork",
    "https://runwayml.com": "AI video editing and generation platform for creatives",
    "https://www.perplexity.ai": "AI search engine that gives accurate answers with cited sources",
    "https://www.alibaba.com": "The world's largest B2B trade platform, connecting buyers and suppliers",
    "https://www.globalsources.com": "Professional B2B sourcing platform focused on Asian manufacturers",
    "https://www.made-in-china.com": "Portal for Chinese manufacturers, with product showcases and trade services",
    "https://www.tradekey.com": "International trade portal connecting importers and exporters worldwide",
    "https://www.exporthub.com": "Global trade network focused on export opportunities",
    "https://www.ec21.com": "Korea's leading B2B trade platform, connecting Asian suppliers",
    "https://www.khanacademy.org": "Free, world-class education covering every subject from primary school to university",
    "https://www.time4learning.com": "Online homeschool courses with a complete K-12 curriculum",
    "https://www.ixl.com": "Personalized learning platform with practice in math, language arts and more",
    "https://www.homeschool.com": "Homeschool resource portal with curricula, resources and community support",
    "https://allinonehomeschool.com": "Completely free Christian homeschool curriculum, from preschool to high school",
    "https://www.teachingtextbooks.com": "Interactive math curriculum designed for homeschooling",
    "https://www.goodandbeautiful.com": "High-quality, low-cost homeschool curricula and resources",
  },
  ko: {
    "https://www.wsj.com": "고품질 경제·정치 보도를 제공하는 미국의 권위 있는 경제지",
    "https://www.nationalreview.com": "미국 보수주의 사상의 중심지, 깊이 있는 정치·문화 분석 제공",
    "https://thefederalist.com": "깊이 있는 분석과 논평으로 알려진 보수 성향 웹 매거진",
    "https://www.dailywire.com": "뉴스와 논평을 제공하는 빠르게 성장 중인 보수 미디어 플랫폼",
    "https://www.foxnews.com": "미국에서 시청률이 가장 높은 케이블 뉴스 채널",
    "https://www.theepochtimes.com": "중국 문제와 국제 뉴스에 집중하는 독립 언론",
    "https://justthenews.com": "사실 보도에 집중하며 독자적인 탐사 보도를 제공하는 뉴스 사이트",
    "https://nypost.com": "빠른 뉴스 보도로 알려진 오랜 역사의 뉴욕 타블로이드지",
    "https://www.youtube.com": "교육·엔터테인먼트 콘텐츠가 풍부한 세계 최대 동영상 공유 플랫폼",
    "https://www.netflix.com": "오리지널 시리즈와 영화를 제공하는 세계적인 스트리밍 서비스",
    "https://www.spotify.com": "팟캐스트와 오디오북도 지원하는 세계 최대 음악 스트리밍 플랫폼",
    "https://www.primevideo.com": "독점 콘텐츠와 영화 대여를 제공하는 아마존의 스트리밍 서비스",
    "https://music.apple.com": "고음질 오디오와 독점 콘텐츠를 제공하는 Apple의 음악 서비스",
    "https://www.twitch.tv": "실시간으로 소통하는 게임 라이브 스트리밍 플랫폼",
    "https://www.whatsapp.com": "음성·영상 통화를 지원하는 세계에서 가장 인기 있는 메신저",
    "https://telegram.org": "대규모 그룹을 지원하는 개인정보 보호 중심의 클라우드 메신저",
    "https://signal.org": "개인정보 보호 전문가들이 널리 추천하는 오픈소스 암호화 메신저",
    "https://discord.com": "음성 채널과 서버 관리를 지원하는 커뮤니티 채팅 플랫폼",
    "https://zoom.us": "웨비나와 온라인 강의를 지원하는 전문 화상 회의 소프트웨어",
    "https://teams.microsoft.com": "오피스 제품군과 프로젝트 관리를 통합한 기업용 협업 플랫폼",
    "https://www.notion.so": "메모, 프로젝트 관리, 지식 베이스를 하나로 합친 올인원 워크스페이스",
    "https://www.canva.com": "템플릿이 풍부해 비전문가도 쉽게 쓰는 온라인 디자인 도구",
    "https://workspace.google.com": "문서, 스프레드시트, 프레젠테이션, 클라우드 저장소를 포함한 Google 오피스 제품군",
    "https://www.figma.com": "실시간 팀 협업을 지원하는 전문 UI/UX 협업 디자인 도구",
    "https://1password.com": "디지털 신원과 민감한 정보를 보호하는 안전한 비밀번호 관리자",
    "https://www.grammarly.com": "영어 문법과 표현을 다듬어 주는 AI 글쓰기 도우미",
    "https://www.desiringgod.org": "존 파이퍼가 설립한 복음주의 사역, 만물 안에서 하나님의 지고하심을 전함",
    "https://www.ligonier.org": "R.C. 스프로울이 설립한 교육 사역, 하나님의 거룩하심과 개혁주의 신학에 집중",
    "https://www.gty.org": "존 맥아더의 교육 사역, 구절별 강해와 성경 공부 제공",
    "https://www.gotquestions.org": "여러 언어로 성경 질문에 답하는 세계 최대 성경 Q&A 사이트",
    "https://www.thegospelcoalition.org": "복음주의 목회자와 신학자들이 함께하는 복음연합 협력 플랫폼",
    "https://www.christianitytoday.com": "전 세계 기독교 소식을 전하는 권위 있는 기독교 뉴스 잡지",
    "https://www.biblegateway.com": "다양한 번역본을 지원하는 세계에서 가장 인기 있는 온라인 성경",
    "https://chat.openai.com": "OpenAI가 개발한 AI 대화 도우미, 글쓰기·코딩·질의응답 지원",
    "https://claude.ai": "Anthropic이 개발한 AI 도우미, 안전하고 유용한 대화에 집중",
    "https://github.com/features/copilot": "실시간 코드 제안과 자동 완성을 제공하는 AI 코딩 도우미",
    "https://www.midjourney.com": "고품질 예술 작품을 만드는 AI 이미지 생성 도구",
    "https://runwayml.com": "크리에이터를 위한 AI 영상 편집·생성 플랫폼",
    "https://www.perplexity.ai": "출처를 함께 제시해 정확한 답을 주는 AI 검색 엔진",
    "https://www.alibaba.com": "구매자와 공급업체를 연결하는 세계 최대 B2B 무역 플랫폼",
    "https://www.globalsources.com": "아시아 제조업체에 특화된 전문 B2B 소싱 플랫폼",
    "https://www.made-in-china.com": "제품 전시와 무역 서비스를 제공하는 중국 제조업체 포털",
    "https://www.tradekey.com": "전 세계 수출입업체를 연결하는 국제 무역 포털",
    "https://www.exporthub.com": "수출 무역 기회에 집중하는 글로벌 무역 네트워크",
    "https://www.ec21.com": "아시아 공급업체를 연결하는 한국의 대표 B2B 무역 플랫폼",
    "https://www.khanacademy.org": "초등학교부터 대학교까지 모든 과목을 다루는 무료 세계적 수준의 교육",
    "https://www.time4learning.com": "K-12 전 과정을 갖춘 온라인 홈스쿨링 과정",
    "https://www.ixl.com": "수학, 언어 등 과목별 연습을 제공하는 맞춤형 학습 플랫폼",
    "https://www.homeschool.com": "커리큘럼, 자료, 커뮤니티 지원을 제공하는 홈스쿨링 자료 포털",
    "https://allinonehomeschool.com": "유치원부터 고등학교까지 완전 무료인 기독교 홈스쿨링 커리큘럼",
    "https://www.teachingtextbooks.com": "홈스쿨링을 위해 설계된 인터랙티브 수학 과정",
    "https://www.goodandbeautiful.com": "고품질·저비용 홈스쿨링 커리큘럼과 자료",
  },
  ja: {
    "https://www.wsj.com": "質の高いビジネス・政治報道を提供する米国の権威ある経済紙",
    "https://www.nationalreview.com": "米国保守思想の拠点。政治・文化を深く分析",
    "https://thefederalist.com": "深い分析と論評で知られる保守系ウェブマガジン",
    "https://www.dailywire.com": "ニュースと論評を届ける急成長中の保守系メディア",
    "https://www.foxnews.com": "米国で最も視聴率の高いケーブルニュースチャンネル",
    "https://www.theepochtimes.com": "中国問題と国際ニュースに注力する独立系メディア",
    "https://justthenews.com": "事実報道に徹し、独自の調査報道を提供するニュースサイト",
    "https://nypost.com": "速報性で知られる歴史あるニューヨークのタブロイド紙",
    "https://www.youtube.com": "教育・エンタメコンテンツが豊富な世界最大の動画共有プラットフォーム",
    "https://www.netflix.com": "オリジナルドラマや映画を配信する世界有数のストリーミングサービス",
    "https://www.spotify.com": "ポッドキャストやオーディオブックにも対応した世界最大の音楽ストリーミング",
    "https://www.primevideo.com": "独占コンテンツと映画レンタルを提供する Amazon のストリーミングサービス",
    "https://music.apple.com": "高音質と独占コンテンツを備えた Apple の音楽サービス",
    "https://www.twitch.tv": "リアルタイムで交流できるゲーム配信プラットフォーム",
    "https://www.whatsapp.com": "音声・ビデオ通話に対応した世界で最も人気のメッセージアプリ",
    "https://telegram.org": "大規模グループに対応した、プライバシー重視のクラウド型メッセージアプリ",
    "https://signal.org": "プライバシーの専門家が広く推奨するオープンソースの暗号化メッセージアプリ",
    "https://discord.com": "ボイスチャンネルとサーバー管理に対応したコミュニティチャット",
    "https://zoom.us": "ウェビナーやオンライン授業に対応したビデオ会議ソフト",
    "https://teams.microsoft.com": "オフィススイートとプロジェクト管理を統合した企業向けコラボレーション基盤",
    "https://www.notion.so": "メモ、プロジェクト管理、ナレッジベースをひとつにしたワークスペース",
    "https://www.canva.com": "テンプレートが豊富で、デザイン初心者でも使いやすいオンラインデザインツール",
    "https://workspace.google.com": "ドキュメント、スプレッドシート、スライド、クラウドストレージを含む Google のオフィススイート",
    "https://www.figma.com": "リアルタイムの共同作業に対応したプロ向け UI/UX デザインツール",
    "https://1password.com": "デジタル ID と機密情報を守る安全なパスワードマネージャー",
    "https://www.grammarly.com": "英文の文法と表現を改善する AI ライティングアシスタント",
    "https://www.desiringgod.org": "ジョン・パイパーが創設した福音派のミニストリー。万物における神の至高性を伝える",
    "https://www.ligonier.org": "R.C. スプロールが創設した教育ミニストリー。神の聖さと改革派神学に注力",
    "https://www.gty.org": "ジョン・マッカーサーの教育ミニストリー。逐節講解と聖書研究を提供",
    "https://www.gotquestions.org": "多言語で聖書の疑問に答える世界最大の聖書 Q&A サイト",
    "https://www.thegospelcoalition.org": "福音派の牧師や神学者が集うゴスペル・コアリションの協働プラットフォーム",
    "https://www.christianitytoday.com": "世界のキリスト教の動向を伝える権威あるキリスト教ニュース誌",
    "https://www.biblegateway.com": "多数の訳に対応した世界で最も人気のオンライン聖書",
    "https://chat.openai.com": "OpenAI が開発した AI 対話アシスタント。文章作成、プログラミング、質問応答に対応",
    "https://claude.ai": "Anthropic が開発した AI アシスタント。安全で役に立つ対話を重視",
    "https://github.com/features/copilot": "リアルタイムのコード提案と自動補完を行う AI プログラミングアシスタント",
    "https://www.midjourney.com": "高品質なアート作品を生み出す AI 画像生成ツール",
    "https://runwayml.com": "クリエイター向けの AI 動画編集・生成プラットフォーム",
    "https://www.perplexity.ai": "出典付きで正確な答えを返す AI 検索エンジン",
    "https://www.alibaba.com": "バイヤーとサプライヤーをつなぐ世界最大の B2B 貿易プラットフォーム",
    "https://www.globalsources.com": "アジアのメーカーに特化したプロ向け B2B 調達プラットフォーム",
    "https://www.made-in-china.com": "製品紹介と貿易サービスを提供する中国メーカーのポータル",
    "https://www.tradekey.com": "世界の輸出入業者をつなぐ国際貿易ポータル",
    "https://www.exporthub.com": "輸出ビジネスの機会に特化したグローバル貿易ネットワーク",
    "https://www.ec21.com": "アジアのサプライヤーをつなぐ韓国の大手 B2B 貿易プラットフォーム",
    "https://www.khanacademy.org": "小学校から大学まで全科目を網羅する、無料で世界水準の教育",
    "https://www.time4learning.com": "K-12 の全カリキュラムを備えたオンラインのホームスクール講座",
    "https://www.ixl.com": "算数・数学や国語などの演習ができる個別最適化学習プラットフォーム",
    "https://www.homeschool.com": "カリキュラム、教材、コミュニティを提供するホームスクール情報ポータル",
    "https://allinonehomeschool.com": "就学前から高校までの完全無料のキリスト教ホームスクールカリキュラム",
    "https://www.teachingtextbooks.com": "ホームスクール向けに設計されたインタラクティブな数学教材",
    "https://www.goodandbeautiful.com": "高品質で低価格なホームスクール用カリキュラムと教材",
  },
  ru: {
    "https://www.wsj.com": "Авторитетная американская деловая газета с качественными материалами о бизнесе и политике",
    "https://www.nationalreview.com": "Ведущее издание американской консервативной мысли с глубоким политическим и культурным анализом",
    "https://thefederalist.com": "Консервативный онлайн-журнал, известный глубоким анализом и комментариями",
    "https://www.dailywire.com": "Быстрорастущая консервативная медиаплатформа с новостями и комментариями",
    "https://www.foxnews.com": "Самый популярный кабельный новостной канал США",
    "https://www.theepochtimes.com": "Независимое издание, специализирующееся на Китае и международных новостях",
    "https://justthenews.com": "Новостной сайт, ориентированный на факты, с собственными расследованиями",
    "https://nypost.com": "Нью-йоркский таблоид с долгой историей, известный оперативными новостями",
    "https://www.youtube.com": "Крупнейшая в мире видеоплатформа с массой образовательного и развлекательного контента",
    "https://www.netflix.com": "Ведущий мировой стриминговый сервис с оригинальными сериалами и фильмами",
    "https://www.spotify.com": "Крупнейший в мире музыкальный стриминг с подкастами и аудиокнигами",
    "https://www.primevideo.com": "Стриминговый сервис Amazon с эксклюзивным контентом и прокатом фильмов",
    "https://music.apple.com": "Музыкальный сервис Apple с высоким качеством звука и эксклюзивами",
    "https://www.twitch.tv": "Платформа игровых трансляций с живым общением",
    "https://www.whatsapp.com": "Самый популярный в мире мессенджер с голосовыми и видеозвонками",
    "https://telegram.org": "Облачный мессенджер с упором на приватность и поддержкой больших групп",
    "https://signal.org": "Мессенджер с открытым кодом и шифрованием, рекомендуемый экспертами по приватности",
    "https://discord.com": "Платформа для общения сообществ с голосовыми каналами и управлением серверами",
    "https://zoom.us": "Профессиональные видеоконференции, вебинары и онлайн-занятия",
    "https://teams.microsoft.com": "Корпоративная платформа для совместной работы с офисным пакетом и управлением проектами",
    "https://www.notion.so": "Единое рабочее пространство: заметки, управление проектами и база знаний",
    "https://www.canva.com": "Простой онлайн-редактор дизайна с множеством шаблонов — подойдёт и непрофессионалам",
    "https://workspace.google.com": "Офисный пакет Google: Документы, Таблицы, Презентации и облачное хранилище",
    "https://www.figma.com": "Профессиональный инструмент для UI/UX-дизайна с совместной работой в реальном времени",
    "https://1password.com": "Надёжный менеджер паролей для защиты цифровой личности и конфиденциальных данных",
    "https://www.grammarly.com": "ИИ-помощник, улучшающий грамматику и стиль текстов на английском",
    "https://www.desiringgod.org": "Евангельское служение, основанное Джоном Пайпером, — о превосходстве Бога во всём",
    "https://www.ligonier.org": "Учительское служение, основанное Р. Ч. Спроулом, — о святости Бога и реформатском богословии",
    "https://www.gty.org": "Учительское служение Джона Макартура: постиховое толкование и изучение Библии",
    "https://www.gotquestions.org": "Крупнейший в мире сайт вопросов и ответов о Библии на многих языках",
    "https://www.thegospelcoalition.org": "«Евангельская коалиция» — площадка для совместной работы евангельских пасторов и богословов",
    "https://www.christianitytoday.com": "Авторитетный христианский новостной журнал о событиях в христианском мире",
    "https://www.biblegateway.com": "Самая популярная онлайн-Библия с множеством переводов",
    "https://chat.openai.com": "ИИ-ассистент от OpenAI для текстов, программирования и ответов на вопросы",
    "https://claude.ai": "ИИ-ассистент от Anthropic, ориентированный на безопасный и полезный диалог",
    "https://github.com/features/copilot": "ИИ-помощник программиста с подсказками кода и автодополнением в реальном времени",
    "https://www.midjourney.com": "ИИ-генератор изображений для создания качественных художественных работ",
    "https://runwayml.com": "ИИ-платформа для монтажа и генерации видео для креативщиков",
    "https://www.perplexity.ai": "ИИ-поисковик, дающий точные ответы со ссылками на источники",
    "https://www.alibaba.com": "Крупнейшая в мире B2B-площадка, соединяющая покупателей и поставщиков",
    "https://www.globalsources.com": "Профессиональная B2B-площадка закупок с фокусом на азиатских производителей",
    "https://www.made-in-china.com": "Портал китайских производителей с витриной товаров и торговыми услугами",
    "https://www.tradekey.com": "Международный торговый портал, соединяющий импортёров и экспортёров по всему миру",
    "https://www.exporthub.com": "Глобальная торговая сеть с фокусом на экспортных возможностях",
    "https://www.ec21.com": "Ведущая корейская B2B-площадка, соединяющая азиатских поставщиков",
    "https://www.khanacademy.org": "Бесплатное образование мирового уровня по всем предметам — от начальной школы до университета",
    "https://www.time4learning.com": "Онлайн-курсы для домашнего обучения с полной программой K-12",
    "https://www.ixl.com": "Персонализированная платформа с упражнениями по математике, языку и другим предметам",
    "https://www.homeschool.com": "Портал для домашнего обучения: программы, материалы и поддержка сообщества",
    "https://allinonehomeschool.com": "Полностью бесплатная христианская программа домашнего обучения — от дошкольников до старшей школы",
    "https://www.teachingtextbooks.com": "Интерактивный курс математики для домашнего обучения",
    "https://www.goodandbeautiful.com": "Качественные и недорогие программы и материалы для домашнего обучения",
  },
};

/** Site description in the page locale; zh-* (and unknown) keep the original copy. */
function siteDescription(site: { url: string; description: string }, locale: string): string {
  return (DESCRIPTIONS_I18N as Record<string, Record<string, string>>)[locale]?.[site.url] ?? site.description;
}

/**
 * Generate metadata for the discovery page (used by Next.js for <head> tags).
 * Requires server-side translation to produce locale-aware title/description.
 */
export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'discovery' });

  return generateBaseMetadata(locale, '/discovery', {
    title: t('discovery.title'),
    description: t('discovery.subtitle'),
  });
}

/**
 * Discovery page Server Component — SSR-converted from client component.
 *
 * Server shell renders all static content unconditionally.
 * DiscoveryClient wraps content and applies embedded-mode CSS class.
 * Uses async params per Next.js 15 pattern.
 */
export default async function DiscoveryPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);
  const t = await getTranslations({ locale });

  return (
    <DiscoveryClient>

        {/* Hero Section */}
        <section className="py-16 px-4 sm:px-6 lg:px-8">
          <div className="max-w-7xl mx-auto text-center">
            <div className="flex items-center justify-center mb-6">
              <div className="p-3 bg-blue-900/50 rounded-full">
                <Globe className="w-8 h-8 text-blue-600" />
              </div>
            </div>
            <h1 className="text-4xl sm:text-5xl font-bold text-foreground mb-4">
              {t('discovery.discovery.title')}
            </h1>
            <p className="text-xl text-muted-foreground mb-8 max-w-3xl mx-auto">
              {t('discovery.discovery.subtitle')}
            </p>
          </div>
        </section>

        {/* News Section */}
        <section className="py-12 px-4 sm:px-6 lg:px-8 bg-muted">
          <div className="max-w-7xl mx-auto">
            <div className="flex items-center mb-8">
              <Newspaper className="w-6 h-6 text-blue-600 mr-3" />
              <div>
                <h2 className="text-2xl font-bold text-foreground">
                  {t('discovery.discovery.sections.news.title')}
                </h2>
                <p className="text-muted-foreground">
                  {t('discovery.discovery.sections.news.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
              {DISCOVERY_DATA.news.map((website, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-card">
                  <a
                    href={website.url}
                    target="_blank"
                    rel="nofollow noopener noreferrer"
                    className="block"
                  >
                    <div className="flex items-start space-x-3">
                      <div className="text-2xl group-hover:scale-110 transition-transform">
                        {website.icon}
                      </div>
                      <div className="flex-1 min-w-0">
                        <h3 className="text-lg font-semibold text-foreground mb-2 truncate">
                          {website.name}
                        </h3>
                        <p className="text-sm text-muted-foreground line-clamp-2">
                          {siteDescription(website, locale)}
                        </p>
                        <div className="flex items-center justify-end mt-2 opacity-0 group-hover:opacity-100 transition-opacity">
                          <ExternalLink className="w-4 h-4 text-blue-600" />
                        </div>
                      </div>
                    </div>
                  </a>
                </Card>
              ))}
            </div>
          </div>
        </section>

        {/* Entertainment Section */}
        <section className="py-12 px-4 sm:px-6 lg:px-8">
          <div className="max-w-7xl mx-auto">
            <div className="flex items-center mb-8">
              <Tv className="w-6 h-6 text-purple-600 mr-3" />
              <div>
                <h2 className="text-2xl font-bold text-foreground">
                  {t('discovery.discovery.sections.entertainment.title')}
                </h2>
                <p className="text-muted-foreground">
                  {t('discovery.discovery.sections.entertainment.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-7 gap-4">
              {DISCOVERY_DATA.entertainment.map((platform, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-card">
                  <a
                    href={platform.url}
                    target="_blank"
                    rel="nofollow noopener noreferrer"
                    className="block text-center"
                  >
                    <div className="text-3xl mb-2 group-hover:scale-110 transition-transform">
                      {platform.icon}
                    </div>
                    <h3 className="text-sm font-medium text-foreground mb-1 truncate">
                      {platform.name}
                    </h3>
                    <p className="text-xs text-muted-foreground line-clamp-2">
                      {siteDescription(platform, locale)}
                    </p>
                    <div className="flex items-center justify-center mt-2 opacity-0 group-hover:opacity-100 transition-opacity">
                      <ExternalLink className="w-3 h-3 text-purple-600" />
                    </div>
                  </a>
                </Card>
              ))}
            </div>
          </div>
        </section>

        {/* Communication Section */}
        <section className="py-12 px-4 sm:px-6 lg:px-8 bg-muted">
          <div className="max-w-7xl mx-auto">
            <div className="flex items-center mb-8">
              <Users className="w-6 h-6 text-green-600 mr-3" />
              <div>
                <h2 className="text-2xl font-bold text-foreground">
                  {t('discovery.discovery.sections.communication.title')}
                </h2>
                <p className="text-muted-foreground">
                  {t('discovery.discovery.sections.communication.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-6 gap-4">
              {DISCOVERY_DATA.communication.map((app, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-card">
                  <a
                    href={app.url}
                    target="_blank"
                    rel="nofollow noopener noreferrer"
                    className="block text-center"
                  >
                    <div className="text-3xl mb-2 group-hover:scale-110 transition-transform">
                      {app.icon}
                    </div>
                    <h3 className="text-sm font-medium text-foreground mb-1 truncate">
                      {app.name}
                    </h3>
                    <p className="text-xs text-muted-foreground line-clamp-2">
                      {siteDescription(app, locale)}
                    </p>
                    <div className="flex items-center justify-center mt-2 opacity-0 group-hover:opacity-100 transition-opacity">
                      <ExternalLink className="w-3 h-3 text-green-600" />
                    </div>
                  </a>
                </Card>
              ))}
            </div>
          </div>
        </section>

        {/* Applications Section */}
        <section className="py-12 px-4 sm:px-6 lg:px-8">
          <div className="max-w-7xl mx-auto">
            <div className="flex items-center mb-8">
              <Globe className="w-6 h-6 text-orange-600 mr-3" />
              <div>
                <h2 className="text-2xl font-bold text-foreground">
                  {t('discovery.discovery.sections.applications.title')}
                </h2>
                <p className="text-muted-foreground">
                  {t('discovery.discovery.sections.applications.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-6 gap-4">
              {DISCOVERY_DATA.applications.map((app, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-card">
                  <a
                    href={app.url}
                    target="_blank"
                    rel="nofollow noopener noreferrer"
                    className="block text-center"
                  >
                    <div className="text-3xl mb-2 group-hover:scale-110 transition-transform">
                      {app.icon}
                    </div>
                    <h3 className="text-sm font-medium text-foreground mb-1 truncate">
                      {app.name}
                    </h3>
                    <p className="text-xs text-muted-foreground line-clamp-2">
                      {siteDescription(app, locale)}
                    </p>
                    <div className="flex items-center justify-center mt-2 opacity-0 group-hover:opacity-100 transition-opacity">
                      <ExternalLink className="w-3 h-3 text-orange-600" />
                    </div>
                  </a>
                </Card>
              ))}
            </div>
          </div>
        </section>

        {/* Christian Resources Section */}
        <section className="py-12 px-4 sm:px-6 lg:px-8 bg-secondary/10">
          <div className="max-w-7xl mx-auto">
            <div className="flex items-center mb-8">
              <BookOpen className="w-6 h-6 text-blue-600 mr-3" />
              <div>
                <h2 className="text-2xl font-bold text-foreground">
                  {t('discovery.discovery.sections.christian.title')}
                </h2>
                <p className="text-muted-foreground">
                  {t('discovery.discovery.sections.christian.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
              {DISCOVERY_DATA.christian.map((resource, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-card">
                  <a
                    href={resource.url}
                    target="_blank"
                    rel="nofollow noopener noreferrer"
                    className="block"
                  >
                    <div className="flex items-start space-x-3">
                      <div className="text-2xl group-hover:scale-110 transition-transform">
                        {resource.icon}
                      </div>
                      <div className="flex-1 min-w-0">
                        <h3 className="text-lg font-semibold text-foreground mb-2 truncate">
                          {resource.name}
                        </h3>
                        <p className="text-sm text-muted-foreground line-clamp-2">
                          {siteDescription(resource, locale)}
                        </p>
                        <div className="flex items-center justify-end mt-2 opacity-0 group-hover:opacity-100 transition-opacity">
                          <ExternalLink className="w-4 h-4 text-blue-600" />
                        </div>
                      </div>
                    </div>
                  </a>
                </Card>
              ))}
            </div>
          </div>
        </section>

        {/* AI Tools Section */}
        <section className="py-12 px-4 sm:px-6 lg:px-8">
          <div className="max-w-7xl mx-auto">
            <div className="flex items-center mb-8">
              <Smartphone className="w-6 h-6 text-indigo-600 mr-3" />
              <div>
                <h2 className="text-2xl font-bold text-foreground">
                  {t('discovery.discovery.sections.aiTools.title')}
                </h2>
                <p className="text-muted-foreground">
                  {t('discovery.discovery.sections.aiTools.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-6 gap-4">
              {DISCOVERY_DATA.aiTools.map((tool, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-card">
                  <a
                    href={tool.url}
                    target="_blank"
                    rel="nofollow noopener noreferrer"
                    className="block text-center"
                  >
                    <div className="text-3xl mb-2 group-hover:scale-110 transition-transform">
                      {tool.icon}
                    </div>
                    <h3 className="text-sm font-medium text-foreground mb-1 truncate">
                      {tool.name}
                    </h3>
                    <p className="text-xs text-muted-foreground line-clamp-2">
                      {siteDescription(tool, locale)}
                    </p>
                    <div className="flex items-center justify-center mt-2 opacity-0 group-hover:opacity-100 transition-opacity">
                      <ExternalLink className="w-3 h-3 text-indigo-600" />
                    </div>
                  </a>
                </Card>
              ))}
            </div>
          </div>
        </section>

        {/* Trade Tools Section */}
        <section className="py-12 px-4 sm:px-6 lg:px-8 bg-muted">
          <div className="max-w-7xl mx-auto">
            <div className="flex items-center mb-8">
              <ShoppingBag className="w-6 h-6 text-amber-600 mr-3" />
              <div>
                <h2 className="text-2xl font-bold text-foreground">
                  {t('discovery.discovery.sections.tradeTools.title')}
                </h2>
                <p className="text-muted-foreground">
                  {t('discovery.discovery.sections.tradeTools.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-6 gap-4">
              {DISCOVERY_DATA.tradeTools.map((tool, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-card">
                  <a
                    href={tool.url}
                    target="_blank"
                    rel="nofollow noopener noreferrer"
                    className="block text-center"
                  >
                    <div className="text-3xl mb-2 group-hover:scale-110 transition-transform">
                      {tool.icon}
                    </div>
                    <h3 className="text-sm font-medium text-foreground mb-1 truncate">
                      {tool.name}
                    </h3>
                    <p className="text-xs text-muted-foreground line-clamp-2">
                      {siteDescription(tool, locale)}
                    </p>
                    <div className="flex items-center justify-center mt-2 opacity-0 group-hover:opacity-100 transition-opacity">
                      <ExternalLink className="w-3 h-3 text-amber-600" />
                    </div>
                  </a>
                </Card>
              ))}
            </div>
          </div>
        </section>

        {/* Homeschool Section */}
        <section className="py-12 px-4 sm:px-6 lg:px-8">
          <div className="max-w-7xl mx-auto">
            <div className="flex items-center mb-8">
              <BookOpen className="w-6 h-6 text-teal-600 mr-3" />
              <div>
                <h2 className="text-2xl font-bold text-foreground">
                  {t('discovery.discovery.sections.homeschool.title')}
                </h2>
                <p className="text-muted-foreground">
                  {t('discovery.discovery.sections.homeschool.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
              {DISCOVERY_DATA.homeschool.map((resource, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-card">
                  <a
                    href={resource.url}
                    target="_blank"
                    rel="nofollow noopener noreferrer"
                    className="block"
                  >
                    <div className="flex items-start space-x-3">
                      <div className="text-2xl group-hover:scale-110 transition-transform">
                        {resource.icon}
                      </div>
                      <div className="flex-1 min-w-0">
                        <h3 className="text-lg font-semibold text-foreground mb-2 truncate">
                          {resource.name}
                        </h3>
                        <p className="text-sm text-muted-foreground line-clamp-2">
                          {siteDescription(resource, locale)}
                        </p>
                        <div className="flex items-center justify-end mt-2 opacity-0 group-hover:opacity-100 transition-opacity">
                          <ExternalLink className="w-4 h-4 text-teal-600" />
                        </div>
                      </div>
                    </div>
                  </a>
                </Card>
              ))}
            </div>
          </div>
        </section>

        {/* Video Playlists Section - Placeholder */}
        <section className="py-12 px-4 sm:px-6 lg:px-8 bg-red-50 dark:bg-red-900/10">
          <div className="max-w-7xl mx-auto">
            <div className="flex items-center mb-8">
              <Video className="w-6 h-6 text-red-600 mr-3" />
              <div>
                <h2 className="text-2xl font-bold text-foreground">
                  {t('discovery.discovery.sections.videoPlaylists.title')}
                </h2>
                <p className="text-muted-foreground">
                  {t('discovery.discovery.sections.videoPlaylists.description')}
                </p>
              </div>
            </div>

            <div className="text-center py-16">
              <div className="text-6xl mb-4">{"🎬"}</div>
              <h3 className="text-xl font-semibold text-foreground mb-2">
                {t('discovery.discovery.sections.videoPlaylists.comingSoon')}
              </h3>
              <p className="text-muted-foreground">
                {t('discovery.discovery.sections.videoPlaylists.comingSoonDesc')}
              </p>
            </div>
          </div>
        </section>

        {/* Footer CTA */}
        <section className="py-16 px-4 sm:px-6 lg:px-8 border-t">
          <div className="max-w-4xl mx-auto text-center">
            <h2 className="text-2xl font-bold text-foreground mb-4">
              {t('discovery.discovery.footer.title')}
            </h2>
            <p className="text-muted-foreground mb-8">
              {t('discovery.discovery.footer.description')}
            </p>
            <div className="flex flex-col sm:flex-row justify-center items-center space-y-4 sm:space-y-0 sm:space-x-4">
              <Link href="/">
                <Button size="lg">
                  {t('discovery.discovery.footer.backHome')}
                </Button>
              </Link>
              {ROUTER_EDITION_ON_SALE && (
                <Link href="/routers">
                  <Button variant="outline" size="lg">
                    {t('discovery.discovery.footer.learnRouters')}
                  </Button>
                </Link>
              )}
            </div>
          </div>
        </section>
      </DiscoveryClient>
  );
}
