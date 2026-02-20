import * as React from "react"

import { Input } from "./input"

export interface DateInputProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'value' | 'onChange'> {
  value?: string; // Expects "yyyy-MM-dd"
  onChange?: (value: string) => void; // Returns "yyyy-MM-dd"
}

const DateInput = React.forwardRef<HTMLInputElement, DateInputProps>(
  ({ className, value, onChange, ...props }, ref) => {
    
    // This component ensures that the value passed to the underlying
    // <input type="date"> is always in the required "yyyy-MM-dd" format.
    // The native date input will then handle displaying it in the user's locale format.
    
    const handleDateChange = (e: React.ChangeEvent<HTMLInputElement>) => {
      const dateValue = e.target.value
      if (onChange) {
        // The value from a date input is already in "yyyy-MM-dd" format.
        onChange(dateValue);
      }
    };
    
    return (
      <Input
        type="date"
        ref={ref}
        className={className}
        value={value || ""}
        onChange={handleDateChange}
        {...props}
      />
    )
  }
)

DateInput.displayName = "DateInput"

export { DateInput } 