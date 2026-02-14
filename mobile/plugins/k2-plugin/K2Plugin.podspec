Pod::Spec.new do |s|
  s.name         = 'K2Plugin'
  s.version      = '0.1.0'
  s.summary      = 'K2 VPN Capacitor Plugin'
  s.license      = 'MIT'
  s.homepage     = 'https://kaitu.io'
  s.author       = 'Kaitu'
  s.source       = { :path => '.' }
  s.source_files = 'ios/Plugin/**/*.{swift,h,m}'
  s.ios.deployment_target = '16.0'
  s.dependency 'Capacitor'
  s.swift_version = '5.9'
end
