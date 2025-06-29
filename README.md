# Cardinham Tennis Club Pay and Play form

Goal is to automate the booking process.

* https://dashboard.stripe.com/test/payment-links/plink_1Rf1aoPR3e9qtWHWaAKM66Gk
* https://buy.stripe.com/test_eVqcN78Wf9TX8vh1cB2oE00

## Current state

https://cardinhamsports.wordpress.com/pay-and-play/

* £7.50 for a one-hour session (for the court) or £10 if you wish to book the court for two hours
* Booking requires phoning 01208 821409 or 07842 612989
* Payments via BACS:  Santander account in name of 'Cardinham Tennis & Sport Club', sort code 09-01-51, account no. 48458300, ref:  'P&P – your surname' for pay-and-play;   or for guests of members please use ref:  'Guest – your surname'.  

## Calendar

Public calendar for members to see when courts are free.

* https://calendar.google.com/calendar/embed?src=cardinhamsports%40gmail.com&ctz=Europe%2FLondon
* https://calendar.google.com/calendar/ical/cardinhamsports%40gmail.com/public/basic.ics

## Web Application

A Go web application that downloads and parses the Google Calendar ICS feed to show real-time court availability with booking details.

## Deployment

This application is deployed to AWS Lambda using GitHub Actions. To deploy:

1. Set up GitHub repository secrets:
   - `GOOGLE_CREDENTIALS_FILE`: The contents of your Google OAuth2 credentials.json file
   - `GOOGLE_TOKEN_FILE`: The contents of your Google OAuth2 token.json file

2. Push to the `main` branch or manually trigger the workflow from the Actions tab

The application will automatically build and deploy to AWS Lambda with the configured domain.