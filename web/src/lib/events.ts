// A simple global event emitter (event bus)

type EventHandler = (data?: unknown) => void;

class EventEmitter {
  private events: { [key:string]: EventHandler[] } = {};

  public on(event: string, listener: EventHandler): void {
    if (!this.events[event]) {
      this.events[event] = [];
    }
    this.events[event].push(listener);
  }

  public off(event: string, listener: EventHandler): void {
    if (!this.events[event]) {
      return;
    }
    this.events[event] = this.events[event].filter(l => l !== listener);
  }

  public emit(event: string, data?: unknown): void {
    if (!this.events[event]) {
      return;
    }
    this.events[event].forEach(listener => listener(data));
  }
}

export const appEvents = new EventEmitter(); 