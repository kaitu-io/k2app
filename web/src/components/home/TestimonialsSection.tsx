import { Card } from '@/components/ui/card';
import { Quote } from 'lucide-react';

export interface Testimonial {
  key: string;
  quote: string;
  author: string;
  tag: string;
}

interface TestimonialsSectionProps {
  sectionTitle: string;
  testimonials: Testimonial[];
}

export default function TestimonialsSection({ sectionTitle, testimonials }: TestimonialsSectionProps) {
  return (
    <section id="testimonials" className="relative z-10 py-20 px-4 sm:px-6 lg:px-8 bg-[rgba(5,5,8,0.6)] backdrop-blur-sm">
      <div className="max-w-7xl mx-auto">
        <div className="text-center mb-14">
          <h2 className="text-3xl font-bold font-mono">{sectionTitle}</h2>
        </div>
        <div className="grid md:grid-cols-3 gap-6">
          {testimonials.map((item) => (
            <Card key={item.key} className="p-6 flex flex-col bg-card border-border">
              <Quote className="w-6 h-6 text-primary/40 mb-4 shrink-0" />
              <p className="text-sm leading-relaxed text-foreground/80 flex-1">{item.quote}</p>
              <div className="mt-4 pt-4 border-t border-border/50">
                <p className="text-sm font-medium text-foreground">{item.author}</p>
                <p className="text-xs text-muted-foreground">{item.tag}</p>
              </div>
            </Card>
          ))}
        </div>
      </div>
    </section>
  );
}
