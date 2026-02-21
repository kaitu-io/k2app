import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import { Link } from '@/i18n/routing';
import { Card } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import Image from 'next/image';
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
      name: "Disney+",
      url: "https://www.disneyplus.com",
      icon: "🏰",
      description: "迪士尼官方流媒体平台，家庭友好的优质内容"
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

  return {
    title: t('discovery.title'),
    description: t('discovery.subtitle'),
  };
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
      <div className="min-h-screen bg-gradient-to-b from-gray-50 to-white dark:from-gray-900 dark:to-gray-800">
        {/* Navigation */}
        <nav className="border-b bg-white/95 dark:bg-gray-900/95 backdrop-blur-sm sticky top-0 z-50">
          <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
            <div className="flex justify-between items-center h-16">
              <div className="flex items-center space-x-2">
                <Image
                  src="/kaitu-icon.png"
                  alt="Kaitu Logo"
                  width={32}
                  height={32}
                  className="rounded-md"
                />
                <Link href="/" className="text-xl font-bold text-gray-900 dark:text-white hover:text-blue-600 transition-colors">
                  {"Kaitu.io"}
                </Link>
              </div>
              <div className="flex items-center space-x-4">
                <Badge variant="outline" className="text-blue-600 border-blue-600">
                  {t('discovery.discovery.title')}
                </Badge>
              </div>
            </div>
          </div>
        </nav>

        {/* Hero Section */}
        <section className="py-16 px-4 sm:px-6 lg:px-8">
          <div className="max-w-7xl mx-auto text-center">
            <div className="flex items-center justify-center mb-6">
              <div className="p-3 bg-blue-100 dark:bg-blue-900 rounded-full">
                <Globe className="w-8 h-8 text-blue-600" />
              </div>
            </div>
            <h1 className="text-4xl sm:text-5xl font-bold text-gray-900 dark:text-white mb-4">
              {t('discovery.discovery.title')}
            </h1>
            <p className="text-xl text-gray-600 dark:text-gray-300 mb-8 max-w-3xl mx-auto">
              {t('discovery.discovery.subtitle')}
            </p>
          </div>
        </section>

        {/* News Section */}
        <section className="py-12 px-4 sm:px-6 lg:px-8 bg-gray-50 dark:bg-gray-800/50">
          <div className="max-w-7xl mx-auto">
            <div className="flex items-center mb-8">
              <Newspaper className="w-6 h-6 text-blue-600 mr-3" />
              <div>
                <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
                  {t('discovery.discovery.sections.news.title')}
                </h2>
                <p className="text-gray-600 dark:text-gray-300">
                  {t('discovery.discovery.sections.news.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
              {DISCOVERY_DATA.news.map((website, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-white dark:bg-gray-900">
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
                        <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2 truncate">
                          {website.name}
                        </h3>
                        <p className="text-sm text-gray-600 dark:text-gray-300 line-clamp-2">
                          {website.description}
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
                <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
                  {t('discovery.discovery.sections.entertainment.title')}
                </h2>
                <p className="text-gray-600 dark:text-gray-300">
                  {t('discovery.discovery.sections.entertainment.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-7 gap-4">
              {DISCOVERY_DATA.entertainment.map((platform, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-white dark:bg-gray-900">
                  <a
                    href={platform.url}
                    target="_blank"
                    rel="nofollow noopener noreferrer"
                    className="block text-center"
                  >
                    <div className="text-3xl mb-2 group-hover:scale-110 transition-transform">
                      {platform.icon}
                    </div>
                    <h3 className="text-sm font-medium text-gray-900 dark:text-white mb-1 truncate">
                      {platform.name}
                    </h3>
                    <p className="text-xs text-gray-500 line-clamp-2">
                      {platform.description}
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
        <section className="py-12 px-4 sm:px-6 lg:px-8 bg-gray-50 dark:bg-gray-800/50">
          <div className="max-w-7xl mx-auto">
            <div className="flex items-center mb-8">
              <Users className="w-6 h-6 text-green-600 mr-3" />
              <div>
                <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
                  {t('discovery.discovery.sections.communication.title')}
                </h2>
                <p className="text-gray-600 dark:text-gray-300">
                  {t('discovery.discovery.sections.communication.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-6 gap-4">
              {DISCOVERY_DATA.communication.map((app, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-white dark:bg-gray-900">
                  <a
                    href={app.url}
                    target="_blank"
                    rel="nofollow noopener noreferrer"
                    className="block text-center"
                  >
                    <div className="text-3xl mb-2 group-hover:scale-110 transition-transform">
                      {app.icon}
                    </div>
                    <h3 className="text-sm font-medium text-gray-900 dark:text-white mb-1 truncate">
                      {app.name}
                    </h3>
                    <p className="text-xs text-gray-500 line-clamp-2">
                      {app.description}
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
                <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
                  {t('discovery.discovery.sections.applications.title')}
                </h2>
                <p className="text-gray-600 dark:text-gray-300">
                  {t('discovery.discovery.sections.applications.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-6 gap-4">
              {DISCOVERY_DATA.applications.map((app, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-white dark:bg-gray-900">
                  <a
                    href={app.url}
                    target="_blank"
                    rel="nofollow noopener noreferrer"
                    className="block text-center"
                  >
                    <div className="text-3xl mb-2 group-hover:scale-110 transition-transform">
                      {app.icon}
                    </div>
                    <h3 className="text-sm font-medium text-gray-900 dark:text-white mb-1 truncate">
                      {app.name}
                    </h3>
                    <p className="text-xs text-gray-500 line-clamp-2">
                      {app.description}
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
        <section className="py-12 px-4 sm:px-6 lg:px-8 bg-blue-50 dark:bg-blue-900/20">
          <div className="max-w-7xl mx-auto">
            <div className="flex items-center mb-8">
              <BookOpen className="w-6 h-6 text-blue-600 mr-3" />
              <div>
                <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
                  {t('discovery.discovery.sections.christian.title')}
                </h2>
                <p className="text-gray-600 dark:text-gray-300">
                  {t('discovery.discovery.sections.christian.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
              {DISCOVERY_DATA.christian.map((resource, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-white dark:bg-gray-900">
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
                        <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2 truncate">
                          {resource.name}
                        </h3>
                        <p className="text-sm text-gray-600 dark:text-gray-300 line-clamp-2">
                          {resource.description}
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
                <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
                  {t('discovery.discovery.sections.aiTools.title')}
                </h2>
                <p className="text-gray-600 dark:text-gray-300">
                  {t('discovery.discovery.sections.aiTools.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-6 gap-4">
              {DISCOVERY_DATA.aiTools.map((tool, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-white dark:bg-gray-900">
                  <a
                    href={tool.url}
                    target="_blank"
                    rel="nofollow noopener noreferrer"
                    className="block text-center"
                  >
                    <div className="text-3xl mb-2 group-hover:scale-110 transition-transform">
                      {tool.icon}
                    </div>
                    <h3 className="text-sm font-medium text-gray-900 dark:text-white mb-1 truncate">
                      {tool.name}
                    </h3>
                    <p className="text-xs text-gray-500 line-clamp-2">
                      {tool.description}
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
        <section className="py-12 px-4 sm:px-6 lg:px-8 bg-gray-50 dark:bg-gray-800/50">
          <div className="max-w-7xl mx-auto">
            <div className="flex items-center mb-8">
              <ShoppingBag className="w-6 h-6 text-amber-600 mr-3" />
              <div>
                <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
                  {t('discovery.discovery.sections.tradeTools.title')}
                </h2>
                <p className="text-gray-600 dark:text-gray-300">
                  {t('discovery.discovery.sections.tradeTools.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-6 gap-4">
              {DISCOVERY_DATA.tradeTools.map((tool, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-white dark:bg-gray-900">
                  <a
                    href={tool.url}
                    target="_blank"
                    rel="nofollow noopener noreferrer"
                    className="block text-center"
                  >
                    <div className="text-3xl mb-2 group-hover:scale-110 transition-transform">
                      {tool.icon}
                    </div>
                    <h3 className="text-sm font-medium text-gray-900 dark:text-white mb-1 truncate">
                      {tool.name}
                    </h3>
                    <p className="text-xs text-gray-500 line-clamp-2">
                      {tool.description}
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
                <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
                  {t('discovery.discovery.sections.homeschool.title')}
                </h2>
                <p className="text-gray-600 dark:text-gray-300">
                  {t('discovery.discovery.sections.homeschool.description')}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
              {DISCOVERY_DATA.homeschool.map((resource, index) => (
                <Card key={index} className="p-4 hover:shadow-lg transition-all duration-300 group cursor-pointer bg-white dark:bg-gray-900">
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
                        <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2 truncate">
                          {resource.name}
                        </h3>
                        <p className="text-sm text-gray-600 dark:text-gray-300 line-clamp-2">
                          {resource.description}
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
                <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
                  {t('discovery.discovery.sections.videoPlaylists.title')}
                </h2>
                <p className="text-gray-600 dark:text-gray-300">
                  {t('discovery.discovery.sections.videoPlaylists.description')}
                </p>
              </div>
            </div>

            <div className="text-center py-16">
              <div className="text-6xl mb-4">{"🎬"}</div>
              <h3 className="text-xl font-semibold text-gray-900 dark:text-white mb-2">
                {t('discovery.discovery.sections.videoPlaylists.comingSoon')}
              </h3>
              <p className="text-gray-600 dark:text-gray-300">
                {t('discovery.discovery.sections.videoPlaylists.comingSoonDesc')}
              </p>
            </div>
          </div>
        </section>

        {/* Footer CTA */}
        <section className="py-16 px-4 sm:px-6 lg:px-8 border-t">
          <div className="max-w-4xl mx-auto text-center">
            <h2 className="text-2xl font-bold text-gray-900 dark:text-white mb-4">
              {t('discovery.discovery.footer.title')}
            </h2>
            <p className="text-gray-600 dark:text-gray-300 mb-8">
              {t('discovery.discovery.footer.description')}
            </p>
            <div className="flex flex-col sm:flex-row justify-center items-center space-y-4 sm:space-y-0 sm:space-x-4">
              <Link href="/">
                <Button size="lg">
                  {t('discovery.discovery.footer.backHome')}
                </Button>
              </Link>
              <Link href="/routers">
                <Button variant="outline" size="lg">
                  {t('discovery.discovery.footer.learnRouters')}
                </Button>
              </Link>
            </div>
          </div>
        </section>
      </div>
    </DiscoveryClient>
  );
}
